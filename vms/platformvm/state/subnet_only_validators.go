// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/google/btree"
)

var (
	_ SubnetOnlyValidators = (*subnetOnlyValidators)(nil)

	ErrAlreadyValidator  = errors.New("already a validator")
	ErrValidatorNotFound = errors.New("validator not found")
	ErrNonZeroWeight     = errors.New("weight must be 0 when minNonce is MaxUint64")
)

type SubnetOnlyValidators interface {
	AddValidator(
		validationID ids.ID,
		subnetID ids.ID,
		nodeID ids.NodeID,
		weight uint64,
		balance uint64,
		endTime uint64,
		pk *bls.PublicKey,
	) error

	ExitValidator(validationID ids.ID) (uint64, error)

	SetValidatorWeight(validationID ids.ID, newWeight uint64, newNonce uint64) (uint64, error)

	IncreaseValidatorBalance(validationID ids.ID, toAdd uint64) error

	AdvanceTime(duration uint64, maxToRemove int) ([]ids.ID, bool)

	Prune(maxToRemove int) ([]ids.ID, bool)

	Write(
		height uint64,
		validatorWeightDiffsDB,
		validatorPublicKeyDiffsDB database.KeyValueWriter,
	) error
}

type subnetOnlyValidator struct {
	ValidationID ids.ID
	SubnetID     ids.ID         `serialize:"true"`
	NodeID       ids.NodeID     `serialize:"true"`
	MinNonce     uint64         `serialize:"true"`
	Weight       uint64         `serialize:"true"`
	Balance      uint64         `serialize:"true"`
	PublicKey    *bls.PublicKey `serialize:"true"`

	// If non-zero, this validator was added via an [Owner] key.
	EndTime uint64 `serialize:"true"`
}

// A *subnetOnlyValidator is considered to be less than another *subnetOnlyValidator when:
//
//  1. If its Balance is less than the other's.
//  2. If the Balances are the same, the one with the lesser NodeID is the
//     lesser one.
func (v *subnetOnlyValidator) Less(than *subnetOnlyValidator) bool {
	if v.Balance < than.Balance {
		return true
	}

	if than.Balance < v.Balance {
		return false
	}

	return bytes.Compare(v.ValidationID[:], than.ValidationID[:]) == -1
}

type subnetOnlyValidatorDiff struct {
	weightDiff *ValidatorWeightDiff
	status     diffValidatorStatus
}

type subnetOnlyValidators struct {
	aggregatedBalance uint64
	calculator        *ValidatorState

	validators    *btree.BTreeG[*subnetOnlyValidator]
	validatorsMap map[ids.ID]*subnetOnlyValidator

	// validationID -> added for that validator since the last db write
	validatorDiffs   map[ids.ID]*subnetOnlyValidatorDiff
	validatorDB      linkeddb.LinkedDB
	validatorManager validators.Manager
}

func newSubnetOnlyValidators(
	calculator *ValidatorState,
	validatorDB linkeddb.LinkedDB,
	validatorManager validators.Manager,
) *subnetOnlyValidators {
	return &subnetOnlyValidators{
		aggregatedBalance: 0,
		calculator:        calculator,

		validators:       btree.NewG(defaultTreeDegree, (*subnetOnlyValidator).Less),
		validatorDiffs:   make(map[ids.ID]*subnetOnlyValidatorDiff),
		validatorDB:      validatorDB,
		validatorManager: validatorManager,
	}
}

func (s *subnetOnlyValidators) AddValidator(
	validationID ids.ID,
	subnetID ids.ID,
	nodeID ids.NodeID,
	weight uint64,
	balance uint64,
	endTime uint64,
	pk *bls.PublicKey,
) error {
	if _, ok := s.validatorsMap[validationID]; ok {
		return ErrAlreadyValidator
	}

	sov := &subnetOnlyValidator{
		ValidationID: validationID,
		SubnetID:     subnetID,
		NodeID:       nodeID,
		MinNonce:     0,
		Weight:       weight,
		Balance:      s.aggregatedBalance + balance,
		PublicKey:    pk,

		EndTime: endTime,
	}
	s.validatorsMap[validationID] = sov
	s.validators.ReplaceOrInsert(sov)
	s.validatorDiffs[validationID] = &subnetOnlyValidatorDiff{
		weightDiff: &ValidatorWeightDiff{
			Decrease: false,
			Amount:   weight,
		},
		status: added,
	}
	s.calculator.Current += 1
	return nil
}

func (s *subnetOnlyValidators) ExitValidator(validationID ids.ID) (uint64, error) {
	sov, ok := s.validatorsMap[validationID]
	if !ok {
		return 0, ErrValidatorNotFound
	}
	delete(s.validatorsMap, validationID)
	s.validators.Delete(sov)

	// Note: s.validatorDiffs is not updated here as the validator is only
	// purged when its weight is set to 0 via SetValidatorWeight()
	s.calculator.Current -= 1

	toRefund := sov.Balance - s.aggregatedBalance
	return toRefund, nil
}

func (s *subnetOnlyValidators) SetValidatorWeight(validationID ids.ID, newWeight uint64, newNonce uint64) (uint64, error) {
	vdr, ok := s.validatorsMap[validationID]
	if !ok {
		return 0, ErrValidatorNotFound
	}

	// Silently drop stale modifications.
	if vdr.MinNonce <= newNonce {
		return 0, nil
	}

	if newNonce == math.MaxUint64 && newWeight != 0 {
		return 0, ErrNonZeroWeight
	}

	currentWeight := s.validatorManager.GetWeight(vdr.SubnetID, vdr.NodeID)

	if newWeight == 0 {
		s.validators.Delete(vdr)

		s.validatorDiffs[validationID] = &subnetOnlyValidatorDiff{
			weightDiff: &ValidatorWeightDiff{
				Decrease: false,
				Amount:   currentWeight,
			},
			status: deleted,
		}
		s.calculator.Current -= 1

		toRefund := vdr.Balance - s.aggregatedBalance
		return toRefund, nil
	}

	if newWeight != currentWeight {
		var weightDiff *ValidatorWeightDiff
		if newWeight > currentWeight {
			weightDiff = &ValidatorWeightDiff{
				Decrease: false,
				Amount:   newWeight - currentWeight,
			}
		} else {
			weightDiff = &ValidatorWeightDiff{
				Decrease: true,
				Amount:   currentWeight - newWeight,
			}
		}

		s.validatorDiffs[validationID] = &subnetOnlyValidatorDiff{
			weightDiff: weightDiff,
		}
	}

	vdr.Weight = newWeight
	vdr.MinNonce = newNonce + 1
	return 0, nil
}

func (s *subnetOnlyValidators) IncreaseValidatorBalance(validationID ids.ID, toAdd uint64) error {
	vdr, ok := s.validatorsMap[validationID]
	if ok {
		s.validators.Delete(vdr)
		vdr.Balance += toAdd
		s.validators.ReplaceOrInsert(vdr)

		return nil
	}

	vdr, err := getSubnetOnlyValidator(s.validatorDB, validationID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrValidatorNotFound, err)
	}

	vdr.Balance = s.aggregatedBalance + toAdd
	s.validators.ReplaceOrInsert(vdr)

	s.calculator.Current += 1
	return nil
}

func (s *subnetOnlyValidators) AdvanceTime(duration uint64, maxToRemove int) ([]ids.ID, bool) {
	fee := s.calculator.CalculateContinuousFee(duration)
	s.aggregatedBalance += fee

	return s.Prune(maxToRemove)
}

func (s *subnetOnlyValidators) Prune(maxToRemove int) ([]ids.ID, bool) {
	removed := []ids.ID{}
	for {
		if len(removed) == maxToRemove {
			break
		}

		validationID, vdr, ok := s.validators.Peek()
		if !ok {
			break
		}

		if vdr.Balance > s.aggregatedBalance {
			break
		}

		s.validators.Pop()
		removed = append(removed, validationID)
	}

	_, vdr, ok := s.validators.Peek()
	moreToRemove := false
	if ok {
		moreToRemove = vdr.Balance <= s.aggregatedBalance
	}

	return removed, moreToRemove
}

func (s *subnetOnlyValidators) Write(
	height uint64,
	validatorWeightDiffsDB,
	validatorPublicKeyDiffsDB database.KeyValueWriter,
) error {
	for validationID, diff := range s.validatorDiffs {
		delete(s.validatorDiffs, validationID)

		vdr, ok := s.validators.Get(validationID)
		if !ok {
			return fmt.Errorf("failed to get subnet only validator: %s", validationID)
		}

		err := writeValidatorWeightDiff(
			validatorWeightDiffsDB,
			vdr.SubnetID,
			height,
			vdr.NodeID,
			diff.weightDiff,
		)
		if err != nil {
			return err
		}

		err = writeValidatorPublicKeyDiff(
			validatorPublicKeyDiffsDB,
			vdr.SubnetID,
			height,
			vdr.NodeID,
			vdr.PublicKey,
		)
		if err != nil {
			return err
		}

		switch diff.status {
		case added:
			err := s.validatorManager.AddStaker(
				vdr.SubnetID,
				vdr.NodeID,
				vdr.PublicKey,
				validationID,
				vdr.Weight,
			)
			if err != nil {
				return fmt.Errorf("failed to add staker: %w", err)
			}

			if err := putSubnetOnlyValidator(s.validatorDB, vdr); err != nil {
				return fmt.Errorf("failed to write subnet only validator: %w", err)
			}
		case deleted:
			if err := deleteSubnetOnlyValidator(s.validatorDB, validationID); err != nil {
				return fmt.Errorf("failed to delete subnet only validator: %w", err)
			}

			continue
		}

		if diff.weightDiff.Decrease {
			if err := s.validatorManager.RemoveWeight(vdr.SubnetID, vdr.NodeID, diff.weightDiff.Amount); err != nil {
				return fmt.Errorf("failed to remove weight: %w", err)
			}
		} else {
			if err := s.validatorManager.AddWeight(vdr.SubnetID, vdr.NodeID, diff.weightDiff.Amount); err != nil {
				return fmt.Errorf("failed to add weight: %w", err)
			}
		}
	}
	return nil
}

func getSubnetOnlyValidator(db database.KeyValueReader, validationID ids.ID) (*subnetOnlyValidator, error) {
	vdrBytes, err := db.Get(validationID[:])
	if err != nil {
		return nil, err
	}

	vdr := &subnetOnlyValidator{
		ValidationID: validationID,
	}
	if _, err = block.GenesisCodec.Unmarshal(vdrBytes, vdr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal subnet only validator: %w", err)
	}
	return vdr, err
}

func putSubnetOnlyValidator(db database.KeyValueWriter, vdr *subnetOnlyValidator) error {
	vdrBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, vdr)
	if err != nil {
		return err
	}

	return db.Put(vdr.ValidationID[:], vdrBytes)
}

func deleteSubnetOnlyValidator(db database.KeyValueDeleter, validationID ids.ID) error {
	return db.Delete(validationID[:])
}

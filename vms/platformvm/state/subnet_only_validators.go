// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
)

type SubnetOnlyValidator struct {
	ValidationID ids.ID
	SubnetID     ids.ID     `serialize:"true"`
	NodeID       ids.NodeID `serialize:"true"`
	MinNonce     uint64     `serialize:"true"`
	Weight       uint64     `serialize:"true"`
	Balance      uint64     `serialize:"true"`
	PublicKey    []byte     `serialize:"true"`

	// If non-zero, this validator was added via an [Owner] key.
	EndTime uint64 `serialize:"true"`
}

// A *SubnetOnlyValidator is considered to be less than another *SubnetOnlyValidator when:
//
//  1. If its Balance is less than the other's.
//  2. If the Balances are the same, the one with the lesser NodeID is the
//     lesser one.
func (v *SubnetOnlyValidator) PayingLess(than *SubnetOnlyValidator) bool {
	if v.Balance < than.Balance {
		return true
	}

	if than.Balance < v.Balance {
		return false
	}

	return bytes.Compare(v.ValidationID[:], than.ValidationID[:]) == -1
}

// A *SubnetOnlyValidator is considered to be less than another *SubnetOnlyValidator when:
//
//  1. If its EndTime is less than the other's.
//  2. If the EndTimes are the same, the one with the lesser NodeID is the
//     lesser one.
func (v *SubnetOnlyValidator) ScheduledLess(than *SubnetOnlyValidator) bool {
	if v.EndTime < than.EndTime {
		return true
	}

	if than.EndTime < v.EndTime {
		return false
	}

	return bytes.Compare(v.ValidationID[:], than.ValidationID[:]) == -1
}

type subnetOnlyValidatorStatus struct {
	Validator *SubnetOnlyValidator
	Added     bool
}

// TODO: Recently used validation IDs need to be persisted for replay protection.
// This is currently not implemented. ACP-77 mandates they are persisted for 48 hours.
type baseSubnetOnlyValidators struct {
	/** current state **/
	accumulatedBalance uint64

	validatorDB database.KeyValueReader        // validationID -> *SubnetOnlyValidator
	validators  map[ids.ID]set.Set[ids.NodeID] // subnetID -> nodeIDs

	payingValidators    *btree.BTreeG[*SubnetOnlyValidator]
	scheduledValidators *btree.BTreeG[*SubnetOnlyValidator]
	activeValidatorMap  map[ids.ID]*SubnetOnlyValidator  // validationID -> *SubnetOnlyValidator
	beginningPublicKeys map[ids.ID]map[ids.NodeID][]byte // subnetID -> nodeID -> publicKey

	/** changes since last db write **/
	newValidationIDs             set.Set[ids.ID]                       // validationIDs
	addedOrModifiedValidationIDs map[ids.ID]*subnetOnlyValidatorStatus // validationID -> {*SubnetOnlyValidator, added}
	deletedValidationIDs         set.Set[ids.ID]                       // validationIDs

	inactiveWeightDiffs  map[ids.ID]ValidatorWeightDiff                // subnetID -> ValidatorWeightDiff
	validatorWeightDiffs map[ids.ID]map[ids.NodeID]ValidatorWeightDiff // subnetID -> nodeID -> ValidatorWeightDiff
	endingPublicKeys     map[ids.ID]map[ids.NodeID][]byte              // subnetID -> nodeID -> publicKey
}

func newBaseSubnetOnlyValidators(validatorDB database.KeyValueReader) *baseSubnetOnlyValidators {
	return &baseSubnetOnlyValidators{
		validatorDB: validatorDB,
		validators:  make(map[ids.ID]set.Set[ids.NodeID]),

		payingValidators:    btree.NewG(defaultTreeDegree, (*SubnetOnlyValidator).PayingLess),
		scheduledValidators: btree.NewG(defaultTreeDegree, (*SubnetOnlyValidator).ScheduledLess),
		activeValidatorMap:  make(map[ids.ID]*SubnetOnlyValidator),
	}
}

func (b *baseSubnetOnlyValidators) GetSubnetOnlyValidator(validationID ids.ID) (*SubnetOnlyValidator, bool, error) {
	vdr, ok := b.activeValidatorMap[validationID]
	if ok {
		return vdr, true, nil
	}

	if b.deletedValidationIDs.Contains(validationID) {
		return nil, false, database.ErrNotFound
	}

	if b.addedOrModifiedValidationIDs[validationID] != nil {
		return b.addedOrModifiedValidationIDs[validationID].Validator, false, nil
	}

	vdr, err := getSubnetOnlyValidator(b.validatorDB, validationID)
	return vdr, false, err
}

func (b *baseSubnetOnlyValidators) AddSubnetOnlyValidator(vdr *SubnetOnlyValidator) error {
	validators := b.validators[vdr.SubnetID]
	if validators.Contains(vdr.NodeID) {
		return errors.New("duplicate nodeID")
	}
	validators.Add(vdr.NodeID)

	b.newValidationIDs.Add(vdr.ValidationID)
	vdr.Balance += b.accumulatedBalance
	b.storeSubnetOnlyValidatorPublicKey(vdr.SubnetID, vdr.NodeID, vdr.PublicKey)
	b.addSubnetOnlyValidator(vdr, true)
	return nil
}

func (b *baseSubnetOnlyValidators) DisableSubnetOnlyValidator(validationID ids.ID) (uint64, error) {
	// Only active validators can be disabled.
	vdr, ok := b.activeValidatorMap[validationID]
	if !ok {
		return 0, errors.New("validator not found")
	}

	// Remove from active validator set and add inactive weight.
	b.payingValidators.Delete(vdr)
	delete(b.activeValidatorMap, vdr.ValidationID)
	b.deleteSubnetOnlyValidatorPublicKey(vdr.SubnetID, vdr.NodeID)
	if err := b.addSubnetInactiveWeightDiff(vdr.SubnetID, false, vdr.Weight); err != nil {
		return 0, err
	}

	// Calculate the amount to be refunded.
	toRefund := uint64(0)
	if vdr.Balance > b.accumulatedBalance {
		toRefund = vdr.Balance - b.accumulatedBalance
	}

	// If the validator was added in the current diff, we can optimize the disk
	// write by not persisting any weight diffs and not call [AddStaker()].
	if status, ok := b.addedOrModifiedValidationIDs[vdr.ValidationID]; ok && status.Added {
		delete(b.validatorWeightDiffs[vdr.SubnetID], vdr.NodeID)
		status.Added = false
		return toRefund, nil
	}

	// Refund the excess AVAX and remove weight.
	return toRefund, b.addValidatorWeightDiff(vdr.SubnetID, vdr.NodeID, true, vdr.Weight)
}

func (b *baseSubnetOnlyValidators) SetSubnetOnlyValidatorWeight(validationID ids.ID, newWeight uint64, newNonce uint64) (uint64, error) {
	vdr, active, err := b.GetSubnetOnlyValidator(validationID)
	if err != nil {
		return 0, err
	}

	if vdr.MinNonce > newNonce {
		// Silently drop stale modifications.
		return 0, nil
	}

	b.pruneSubnetOnlyValidator(vdr)

	if newWeight == 0 {
		toRefund := uint64(0)
		if vdr.Balance > b.accumulatedBalance {
			toRefund = vdr.Balance - b.accumulatedBalance
		}

		b.payingValidators.Delete(vdr)
		delete(b.activeValidatorMap, vdr.ValidationID)
		b.deletedValidationIDs.Add(validationID)
		b.deleteSubnetOnlyValidatorPublicKey(vdr.SubnetID, vdr.NodeID)

		if status, ok := b.addedOrModifiedValidationIDs[vdr.ValidationID]; ok {
			delete(b.addedOrModifiedValidationIDs, vdr.ValidationID)
			if status.Added {
				delete(b.validatorWeightDiffs[vdr.SubnetID], vdr.NodeID)
				return toRefund, nil
			}
		}

		if !active {
			return 0, b.addSubnetInactiveWeightDiff(vdr.SubnetID, true, vdr.Weight)
		}

		return toRefund, b.addValidatorWeightDiff(vdr.SubnetID, vdr.NodeID, true, vdr.Weight)
	}

	decrease := vdr.Weight > newWeight
	weightDiff := math.AbsDiff[uint64](vdr.Weight, newWeight)
	vdr.Weight = newWeight
	vdr.MinNonce = newNonce + 1

	if !active {
		if vdr.EndTime != 0 {
			b.scheduledValidators.ReplaceOrInsert(vdr)
		}
		b.storeSubnetOnlyValidatorStatus(vdr, false)
		return 0, b.addSubnetInactiveWeightDiff(vdr.SubnetID, decrease, weightDiff)
	}

	b.addSubnetOnlyValidator(vdr, false)
	return 0, b.addValidatorWeightDiff(vdr.SubnetID, vdr.NodeID, decrease, weightDiff)
}

func (b *baseSubnetOnlyValidators) IncreaseSubnetOnlyValidatorBalance(validationID ids.ID, toAdd uint64) error {
	vdr, ok := b.activeValidatorMap[validationID]
	if ok {
		b.pruneSubnetOnlyValidator(vdr)
		vdr.Balance += toAdd
		b.addSubnetOnlyValidator(vdr, false)
		return nil
	}

	if b.deletedValidationIDs.Contains(validationID) {
		return errors.New("attempted to increase balance of deleted validator")
	}

	status, ok := b.addedOrModifiedValidationIDs[validationID]
	if ok {
		vdr = status.Validator
	}
	if !ok {
		var err error
		vdr, err = getSubnetOnlyValidator(b.validatorDB, validationID)
		if err != nil {
			return fmt.Errorf("failed to read subnet only validator from db: %w", err)
		}
	}

	b.pruneSubnetOnlyValidator(vdr)
	vdr.Balance = b.accumulatedBalance + toAdd
	b.addSubnetOnlyValidator(vdr, true)
	b.storeSubnetOnlyValidatorPublicKey(vdr.SubnetID, vdr.NodeID, vdr.PublicKey)

	if err := b.addSubnetInactiveWeightDiff(vdr.SubnetID, true, vdr.Weight); err != nil {
		return err
	}

	if !status.Added && b.newValidationIDs.Contains(vdr.ValidationID) {
		status.Added = true
		delete(b.validatorWeightDiffs[vdr.SubnetID], vdr.NodeID)
		return nil
	}

	return b.addValidatorWeightDiff(vdr.SubnetID, vdr.NodeID, false, vdr.Weight)
}

func (b *baseSubnetOnlyValidators) GetAccumulatedBalance() uint64 {
	return b.accumulatedBalance
}

func (b *baseSubnetOnlyValidators) GetPayingSubnetOnlyValidatorIterator() iterator.Iterator[*SubnetOnlyValidator] {
	return iterator.FromTree(b.payingValidators)
}

func (b *baseSubnetOnlyValidators) GetScheduledSubnetOnlyValidatorIterator() iterator.Iterator[*SubnetOnlyValidator] {
	return iterator.FromTree(b.scheduledValidators)
}

func (b *baseSubnetOnlyValidators) Write(
	height uint64,
	validatorDB database.Database,
	validatorWeightDiffsDB,
	validatorPublicKeyDiffsDB database.KeyValueWriter,
	validatorManager validators.Manager,
) error {
	for validationID, vdr := range b.addedOrModifiedValidationIDs {
		delete(b.addedOrModifiedValidationIDs, validationID)
		if err := putSubnetOnlyValidator(validatorDB, vdr.Validator); err != nil {
			return fmt.Errorf("failed to write SoV %s: %w", validationID, err)
		}

		publicKey := bls.PublicKeyFromValidUncompressedBytes(vdr.Validator.PublicKey)
		if vdr.Added {
			err := validatorManager.AddStaker(
				vdr.Validator.SubnetID,
				vdr.Validator.NodeID,
				publicKey,
				validationID,
				vdr.Validator.Weight,
			)
			if err != nil {
				return fmt.Errorf("failed to add staker for SoV %s: %w", validationID, err)
			}
		}
	}

	for subnetID, weightDiff := range b.inactiveWeightDiffs {
		delete(b.inactiveWeightDiffs, subnetID)
		weightDiff := weightDiff

		err := writeValidatorWeightDiff(
			validatorWeightDiffsDB,
			subnetID,
			height,
			ids.EmptyNodeID,
			&weightDiff,
		)
		if err != nil {
			return fmt.Errorf("failed to write inactive weight diff for subnet %s: %w", subnetID, err)
		}

		if weightDiff.Decrease {
			if err := validatorManager.RemoveWeight(subnetID, ids.EmptyNodeID, weightDiff.Amount); err != nil {
				return fmt.Errorf("failed to remove inactive weight: %w", err)
			}
		} else {
			_, ok := validatorManager.GetValidator(subnetID, ids.EmptyNodeID)
			if !ok {
				if err := validatorManager.AddStaker(subnetID, ids.EmptyNodeID, nil, ids.Empty, weightDiff.Amount); err != nil {
					return fmt.Errorf("failed to add inactive staker: %w", err)
				}
			} else {
				if err := validatorManager.AddWeight(subnetID, ids.EmptyNodeID, weightDiff.Amount); err != nil {
					return fmt.Errorf("failed to add inactive weight: %w", err)
				}
			}
		}
	}

	for subnetID, validatorWeightDiffs := range b.validatorWeightDiffs {
		delete(b.validatorWeightDiffs, subnetID)

		for nodeID, weightDiff := range validatorWeightDiffs {
			weightDiff := weightDiff

			err := writeValidatorWeightDiff(
				validatorWeightDiffsDB,
				subnetID,
				height,
				nodeID,
				&weightDiff,
			)
			if err != nil {
				return fmt.Errorf("failed to write weight diff for subnet %s nodeID %s: %w", subnetID, nodeID, err)
			}

			if weightDiff.Decrease {
				if err := validatorManager.RemoveWeight(subnetID, nodeID, weightDiff.Amount); err != nil {
					return fmt.Errorf("failed to remove weight for subnet %s nodeID %s: %w", subnetID, nodeID, err)
				}
			} else {
				if err := validatorManager.AddWeight(subnetID, nodeID, weightDiff.Amount); err != nil {
					return fmt.Errorf("failed to add weight for subnet %s nodeID %s: %w", subnetID, nodeID, err)
				}
			}
		}
	}

	for validationID := range b.deletedValidationIDs {
		b.deletedValidationIDs.Remove(validationID)

		if err := deleteSubnetOnlyValidator(validatorDB, validationID); err != nil {
			return fmt.Errorf("failed to delete SoV %s: %w", validationID, err)
		}
	}

	for subnetID, nodeIDKeys := range b.endingPublicKeys {
		for nodeID, publicKey := range nodeIDKeys {
			if b.beginningPublicKeys != nil && b.beginningPublicKeys[subnetID] != nil && bytes.Equal(b.beginningPublicKeys[subnetID][nodeID], publicKey) {
				continue
			}

			publicKey := bls.PublicKeyFromValidUncompressedBytes(publicKey)
			err := writeValidatorPublicKeyDiff(
				validatorPublicKeyDiffsDB,
				subnetID,
				height,
				nodeID,
				publicKey,
			)
			if err != nil {
				return fmt.Errorf("failed to write public key diff: %w", err)
			}
		}
	}

	b.beginningPublicKeys = b.endingPublicKeys
	b.endingPublicKeys = nil

	return nil
}

func (b *baseSubnetOnlyValidators) deleteSubnetOnlyValidatorPublicKey(subnetID ids.ID, nodeID ids.NodeID) {
	if b.endingPublicKeys == nil {
		return
	}

	if b.endingPublicKeys[subnetID] == nil {
		return
	}

	delete(b.endingPublicKeys[subnetID], nodeID)
}

func (b *baseSubnetOnlyValidators) storeSubnetOnlyValidatorPublicKey(subnetID ids.ID, nodeID ids.NodeID, publicKeyBytes []byte) {
	if b.endingPublicKeys == nil {
		b.endingPublicKeys = map[ids.ID]map[ids.NodeID][]byte{
			subnetID: {
				nodeID: publicKeyBytes,
			},
		}
		return
	}

	if b.endingPublicKeys[subnetID] == nil {
		b.endingPublicKeys[subnetID] = map[ids.NodeID][]byte{
			nodeID: publicKeyBytes,
		}
		return
	}

	b.endingPublicKeys[subnetID][nodeID] = publicKeyBytes
}

func (b *baseSubnetOnlyValidators) storeSubnetOnlyValidatorStatus(vdr *SubnetOnlyValidator, added bool) {
	if b.addedOrModifiedValidationIDs == nil {
		b.addedOrModifiedValidationIDs = map[ids.ID]*subnetOnlyValidatorStatus{
			vdr.ValidationID: {
				Validator: vdr,
				Added:     added,
			},
		}
		return
	}

	status, ok := b.addedOrModifiedValidationIDs[vdr.ValidationID]
	if ok {
		status.Validator = vdr
		return
	}

	b.addedOrModifiedValidationIDs[vdr.ValidationID] = &subnetOnlyValidatorStatus{
		Validator: vdr,
		Added:     added,
	}
}

func (b *baseSubnetOnlyValidators) addSubnetOnlyValidator(vdr *SubnetOnlyValidator, added bool) {
	b.payingValidators.ReplaceOrInsert(vdr)
	if vdr.EndTime != 0 {
		b.scheduledValidators.ReplaceOrInsert(vdr)
	}
	b.activeValidatorMap[vdr.ValidationID] = vdr
	b.storeSubnetOnlyValidatorStatus(vdr, added)
}

func (b *baseSubnetOnlyValidators) pruneSubnetOnlyValidator(vdr *SubnetOnlyValidator) {
	b.payingValidators.Delete(vdr)
	if vdr.EndTime != 0 {
		b.scheduledValidators.Delete(vdr)
	}
}

func (b *baseSubnetOnlyValidators) addSubnetInactiveWeightDiff(subnetID ids.ID, decrease bool, amount uint64) error {
	if b.inactiveWeightDiffs == nil {
		b.inactiveWeightDiffs = map[ids.ID]ValidatorWeightDiff{
			subnetID: {
				Decrease: decrease,
				Amount:   amount,
			},
		}
		return nil
	}

	diff := b.inactiveWeightDiffs[subnetID]
	if err := diff.Add(decrease, amount); err != nil {
		return err
	}

	if diff.Amount == 0 {
		delete(b.inactiveWeightDiffs, subnetID)
		return nil
	}
	b.inactiveWeightDiffs[subnetID] = diff
	return nil
}

func (b *baseSubnetOnlyValidators) addValidatorWeightDiff(subnetID ids.ID, nodeID ids.NodeID, decrease bool, amount uint64) error {
	if b.validatorWeightDiffs == nil {
		b.validatorWeightDiffs = map[ids.ID]map[ids.NodeID]ValidatorWeightDiff{
			subnetID: {
				nodeID: {
					Decrease: decrease,
					Amount:   amount,
				},
			},
		}
		return nil
	}

	if b.validatorWeightDiffs[subnetID] == nil {
		b.validatorWeightDiffs[subnetID] = map[ids.NodeID]ValidatorWeightDiff{
			nodeID: {
				Decrease: decrease,
				Amount:   amount,
			},
		}
		return nil
	}

	diff := b.validatorWeightDiffs[subnetID][nodeID]
	if err := diff.Add(decrease, amount); err != nil {
		return err
	}
	if diff.Amount == 0 {
		delete(b.validatorWeightDiffs[subnetID], nodeID)
		return nil
	}
	b.validatorWeightDiffs[subnetID][nodeID] = diff
	return nil
}

func getSubnetOnlyValidator(db database.KeyValueReader, validationID ids.ID) (*SubnetOnlyValidator, error) {
	vdrBytes, err := db.Get(validationID[:])
	if err != nil {
		return nil, err
	}

	vdr := &SubnetOnlyValidator{ValidationID: validationID}
	if _, err = block.GenesisCodec.Unmarshal(vdrBytes, vdr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal subnet only validator: %w", err)
	}
	return vdr, err
}

func putSubnetOnlyValidator(db database.KeyValueWriter, vdr *SubnetOnlyValidator) error {
	vdrBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, vdr)
	if err != nil {
		return err
	}

	return db.Put(vdr.ValidationID[:], vdrBytes)
}

func deleteSubnetOnlyValidator(db database.KeyValueDeleter, validationID ids.ID) error {
	return db.Delete(validationID[:])
}

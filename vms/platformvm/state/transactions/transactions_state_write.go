// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func (s *state) WriteTxs() error {
	errs := wrappers.Errs{}
	errs.Add(
		s.writeCurrentStakers(),
		s.writePendingStakers(),
		s.writeUptimes(),
		s.writeTXs(),
		s.writeRewardUTXOs(),
		s.writeUTXOs(),
		s.writeSubnets(),
		s.writeChains(),
	)
	return errs.Err
}

func (s *state) CloseTxs() error {
	errs := wrappers.Errs{}
	errs.Add(
		s.pendingSubnetValidatorBaseDB.Close(),
		s.pendingDelegatorBaseDB.Close(),
		s.pendingValidatorBaseDB.Close(),
		s.pendingValidatorsDB.Close(),
		s.currentSubnetValidatorBaseDB.Close(),
		s.currentDelegatorBaseDB.Close(),
		s.currentValidatorBaseDB.Close(),
		s.currentValidatorsDB.Close(),
		s.validatorsDB.Close(),
		s.txDB.Close(),
		s.rewardUTXODB.Close(),
		s.utxoDB.Close(),
		s.subnetBaseDB.Close(),
		s.chainDB.Close(),
	)
	return errs.Err
}

type currentValidatorState struct {
	txID        ids.ID
	lastUpdated time.Time

	UpDuration      time.Duration `serialize:"true"`
	LastUpdated     uint64        `serialize:"true"` // Unix time in seconds
	PotentialReward uint64        `serialize:"true"`
}

func (s *state) writeCurrentStakers() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to write current stakers with: %w", err)
		}
	}()

	weightDiffs := make(map[ids.ID]map[ids.NodeID]*ValidatorWeightDiff) // subnetID -> nodeID -> weightDiff
	for _, currentStaker := range s.addedCurrentStakers {
		txID := currentStaker.AddStakerTx.ID()
		potentialReward := currentStaker.PotentialReward

		var (
			subnetID ids.ID
			nodeID   ids.NodeID
			weight   uint64
		)
		switch tx := currentStaker.AddStakerTx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			var vdrBytes []byte
			startTime := tx.StartTime()
			vdr := &currentValidatorState{
				txID:        txID,
				lastUpdated: startTime,

				UpDuration:      0,
				LastUpdated:     uint64(startTime.Unix()),
				PotentialReward: potentialReward,
			}

			vdrBytes, err = unsigned.GenCodec.Marshal(unsigned.Version, vdr)
			if err != nil {
				return
			}

			if err = s.currentValidatorList.Put(txID[:], vdrBytes); err != nil {
				return
			}
			s.uptimes[tx.Validator.NodeID] = vdr

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddDelegatorTx:
			if err = database.PutUInt64(s.currentDelegatorList, txID[:], potentialReward); err != nil {
				return
			}

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddSubnetValidatorTx:
			if err = s.currentSubnetValidatorList.Put(txID[:], nil); err != nil {
				return
			}

			subnetID = tx.Validator.Subnet
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		default:
			return unsigned.ErrWrongTxType
		}

		subnetDiffs, ok := weightDiffs[subnetID]
		if !ok {
			subnetDiffs = make(map[ids.NodeID]*ValidatorWeightDiff)
			weightDiffs[subnetID] = subnetDiffs
		}

		nodeDiff, ok := subnetDiffs[nodeID]
		if !ok {
			nodeDiff = &ValidatorWeightDiff{}
			subnetDiffs[nodeID] = nodeDiff
		}

		var newWeight uint64
		newWeight, err = safemath.Add64(nodeDiff.Amount, weight)
		if err != nil {
			return err
		}
		nodeDiff.Amount = newWeight
	}
	s.addedCurrentStakers = nil

	for _, tx := range s.deletedCurrentStakers {
		var (
			db       database.KeyValueDeleter
			subnetID ids.ID
			nodeID   ids.NodeID
			weight   uint64
		)
		switch tx := tx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			db = s.currentValidatorList

			delete(s.uptimes, tx.Validator.NodeID)
			delete(s.updatedUptimes, tx.Validator.NodeID)

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddDelegatorTx:
			db = s.currentDelegatorList

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddSubnetValidatorTx:
			db = s.currentSubnetValidatorList

			subnetID = tx.Validator.Subnet
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		default:
			return unsigned.ErrWrongTxType
		}

		txID := tx.ID()
		if err = db.Delete(txID[:]); err != nil {
			return
		}

		subnetDiffs, ok := weightDiffs[subnetID]
		if !ok {
			subnetDiffs = make(map[ids.NodeID]*ValidatorWeightDiff)
			weightDiffs[subnetID] = subnetDiffs
		}

		nodeDiff, ok := subnetDiffs[nodeID]
		if !ok {
			nodeDiff = &ValidatorWeightDiff{}
			subnetDiffs[nodeID] = nodeDiff
		}

		if nodeDiff.Decrease {
			var newWeight uint64
			newWeight, err = safemath.Add64(nodeDiff.Amount, weight)
			if err != nil {
				return
			}
			nodeDiff.Amount = newWeight
		} else {
			nodeDiff.Decrease = nodeDiff.Amount < weight
			nodeDiff.Amount = safemath.Diff64(nodeDiff.Amount, weight)
		}
	}
	s.deletedCurrentStakers = nil

	for subnetID, nodeUpdates := range weightDiffs {
		var prefixBytes []byte
		prefixStruct := heightWithSubnet{
			Height:   s.DataState.GetHeight(),
			SubnetID: subnetID,
		}
		prefixBytes, err = unsigned.GenCodec.Marshal(unsigned.Version, prefixStruct)
		if err != nil {
			return
		}
		rawDiffDB := prefixdb.New(prefixBytes, s.validatorDiffsDB)
		diffDB := linkeddb.NewDefault(rawDiffDB)
		for nodeID, nodeDiff := range nodeUpdates {
			if nodeDiff.Amount == 0 {
				delete(nodeUpdates, nodeID)
				continue
			}

			if subnetID == constants.PrimaryNetworkID || s.cfg.WhitelistedSubnets.Contains(subnetID) {
				if nodeDiff.Decrease {
					err = s.cfg.Validators.RemoveWeight(subnetID, nodeID, nodeDiff.Amount)
				} else {
					err = s.cfg.Validators.AddWeight(subnetID, nodeID, nodeDiff.Amount)
				}
				if err != nil {
					return
				}
			}

			var nodeDiffBytes []byte
			nodeDiffBytes, err = unsigned.GenCodec.Marshal(unsigned.Version, nodeDiff)
			if err != nil {
				return err
			}

			// Copy so value passed into [Put] doesn't get overwritten next iteration
			nodeID := nodeID
			if err := diffDB.Put(nodeID[:], nodeDiffBytes); err != nil {
				return err
			}
		}
		s.validatorDiffsCache.Put(string(prefixBytes), nodeUpdates)
	}

	// Attempt to update the stake metrics
	primaryValidators, ok := s.cfg.Validators.GetValidators(constants.PrimaryNetworkID)
	if !ok {
		return nil
	}
	weight, _ := primaryValidators.GetWeight(s.ctx.NodeID)
	s.localStake.Set(float64(weight))
	s.totalStake.Set(float64(primaryValidators.Weight()))
	return nil
}

func (s *state) writePendingStakers() error {
	for _, tx := range s.addedPendingStakers {
		var db database.KeyValueWriter
		switch tx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			db = s.pendingValidatorList
		case *unsigned.AddDelegatorTx:
			db = s.pendingDelegatorList
		case *unsigned.AddSubnetValidatorTx:
			db = s.pendingSubnetValidatorList
		default:
			return unsigned.ErrWrongTxType
		}

		txID := tx.ID()
		if err := db.Put(txID[:], nil); err != nil {
			return fmt.Errorf("failed to write pending stakers with: %w", err)
		}
	}
	s.addedPendingStakers = nil

	for _, tx := range s.deletedPendingStakers {
		var db database.KeyValueDeleter
		switch tx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			db = s.pendingValidatorList
		case *unsigned.AddDelegatorTx:
			db = s.pendingDelegatorList
		case *unsigned.AddSubnetValidatorTx:
			db = s.pendingSubnetValidatorList
		default:
			return unsigned.ErrWrongTxType
		}

		txID := tx.ID()
		if err := db.Delete(txID[:]); err != nil {
			return fmt.Errorf("failed to write pending stakers with: %w", err)
		}
	}
	s.deletedPendingStakers = nil
	return nil
}

func (s *state) writeUptimes() error {
	for nodeID := range s.updatedUptimes {
		delete(s.updatedUptimes, nodeID)

		uptime := s.uptimes[nodeID]
		uptime.LastUpdated = uint64(uptime.lastUpdated.Unix())

		uptimeBytes, err := unsigned.GenCodec.Marshal(unsigned.Version, uptime)
		if err != nil {
			return fmt.Errorf("failed to write uptimes with: %w", err)
		}

		if err := s.currentValidatorList.Put(uptime.txID[:], uptimeBytes); err != nil {
			return fmt.Errorf("failed to write uptimes with: %w", err)
		}
	}
	return nil
}

func (s *state) writeTXs() error {
	for txID, txStatus := range s.addedTxs {
		txID := txID

		stx := txBytesAndStatus{
			Tx:     txStatus.Tx.Bytes(),
			Status: txStatus.Status,
		}

		txBytes, err := unsigned.GenCodec.Marshal(unsigned.Version, &stx)
		if err != nil {
			return fmt.Errorf("failed to write txs with: %w", err)
		}

		delete(s.addedTxs, txID)
		s.txCache.Put(txID, txStatus)
		if err := s.txDB.Put(txID[:], txBytes); err != nil {
			return fmt.Errorf("failed to write txs with: %w", err)
		}
	}
	return nil
}

func (s *state) writeRewardUTXOs() error {
	for txID, utxos := range s.addedRewardUTXOs {
		delete(s.addedRewardUTXOs, txID)
		s.rewardUTXOsCache.Put(txID, utxos)
		rawTxDB := prefixdb.New(txID[:], s.rewardUTXODB)
		txDB := linkeddb.NewDefault(rawTxDB)

		for _, utxo := range utxos {
			utxoBytes, err := unsigned.GenCodec.Marshal(unsigned.Version, utxo)
			if err != nil {
				return fmt.Errorf("failed to write reward UTXOs with: %w", err)
			}
			utxoID := utxo.InputID()
			if err := txDB.Put(utxoID[:], utxoBytes); err != nil {
				return fmt.Errorf("failed to write reward UTXOs with: %w", err)
			}
		}
	}
	return nil
}

func (s *state) writeUTXOs() error {
	for utxoID, utxo := range s.modifiedUTXOs {
		delete(s.modifiedUTXOs, utxoID)

		if utxo == nil {
			if err := s.utxoState.DeleteUTXO(utxoID); err != nil {
				return fmt.Errorf("failed to write UTXOs with: %w", err)
			}
			continue
		}
		if err := s.utxoState.PutUTXO(utxoID, utxo); err != nil {
			return fmt.Errorf("failed to write UTXOs with: %w", err)
		}
	}
	return nil
}

func (s *state) writeSubnets() error {
	for _, subnet := range s.addedSubnets {
		subnetID := subnet.ID()

		if err := s.subnetDB.Put(subnetID[:], nil); err != nil {
			return fmt.Errorf("failed to write current subnets with: %w", err)
		}
	}
	s.addedSubnets = nil
	return nil
}

func (s *state) writeChains() error {
	for subnetID, chains := range s.addedChains {
		for _, chain := range chains {
			chainDB := s.getChainDB(subnetID)

			chainID := chain.ID()
			if err := chainDB.Put(chainID[:], nil); err != nil {
				return fmt.Errorf("failed to write chains with: %w", err)
			}
		}
		delete(s.addedChains, subnetID)
	}
	return nil
}

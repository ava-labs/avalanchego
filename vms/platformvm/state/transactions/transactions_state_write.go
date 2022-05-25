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

func (ts *state) WriteTxs() error {
	errs := wrappers.Errs{}
	errs.Add(
		ts.writeCurrentStakers(),
		ts.writePendingStakers(),
		ts.writeUptimes(),
		ts.writeTXs(),
		ts.writeRewardUTXOs(),
		ts.writeUTXOs(),
		ts.writeSubnets(),
		ts.writeChains(),
	)
	return errs.Err
}

func (ts *state) CloseTxs() error {
	errs := wrappers.Errs{}
	errs.Add(
		ts.pendingSubnetValidatorBaseDB.Close(),
		ts.pendingDelegatorBaseDB.Close(),
		ts.pendingValidatorBaseDB.Close(),
		ts.pendingValidatorsDB.Close(),
		ts.currentSubnetValidatorBaseDB.Close(),
		ts.currentDelegatorBaseDB.Close(),
		ts.currentValidatorBaseDB.Close(),
		ts.currentValidatorsDB.Close(),
		ts.validatorsDB.Close(),
		ts.txDB.Close(),
		ts.rewardUTXODB.Close(),
		ts.utxoDB.Close(),
		ts.subnetBaseDB.Close(),
		ts.chainDB.Close(),
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

func (ts *state) writeCurrentStakers() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to write current stakers with: %w", err)
		}
	}()

	weightDiffs := make(map[ids.ID]map[ids.NodeID]*ValidatorWeightDiff) // subnetID -> nodeID -> weightDiff
	for _, currentStaker := range ts.addedCurrentStakers {
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

			if err = ts.currentValidatorList.Put(txID[:], vdrBytes); err != nil {
				return
			}
			ts.uptimes[tx.Validator.NodeID] = vdr

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddDelegatorTx:
			if err = database.PutUInt64(ts.currentDelegatorList, txID[:], potentialReward); err != nil {
				return
			}

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddSubnetValidatorTx:
			if err = ts.currentSubnetValidatorList.Put(txID[:], nil); err != nil {
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
	ts.addedCurrentStakers = nil

	for _, tx := range ts.deletedCurrentStakers {
		var (
			db       database.KeyValueDeleter
			subnetID ids.ID
			nodeID   ids.NodeID
			weight   uint64
		)
		switch tx := tx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			db = ts.currentValidatorList

			delete(ts.uptimes, tx.Validator.NodeID)
			delete(ts.updatedUptimes, tx.Validator.NodeID)

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddDelegatorTx:
			db = ts.currentDelegatorList

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddSubnetValidatorTx:
			db = ts.currentSubnetValidatorList

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
	ts.deletedCurrentStakers = nil

	for subnetID, nodeUpdates := range weightDiffs {
		var prefixBytes []byte
		prefixStruct := heightWithSubnet{
			Height:   ts.DataState.GetHeight(),
			SubnetID: subnetID,
		}
		prefixBytes, err = unsigned.GenCodec.Marshal(unsigned.Version, prefixStruct)
		if err != nil {
			return
		}
		rawDiffDB := prefixdb.New(prefixBytes, ts.validatorDiffsDB)
		diffDB := linkeddb.NewDefault(rawDiffDB)
		for nodeID, nodeDiff := range nodeUpdates {
			if nodeDiff.Amount == 0 {
				delete(nodeUpdates, nodeID)
				continue
			}

			if subnetID == constants.PrimaryNetworkID || ts.cfg.WhitelistedSubnets.Contains(subnetID) {
				if nodeDiff.Decrease {
					err = ts.cfg.Validators.RemoveWeight(subnetID, nodeID, nodeDiff.Amount)
				} else {
					err = ts.cfg.Validators.AddWeight(subnetID, nodeID, nodeDiff.Amount)
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
		ts.validatorDiffsCache.Put(string(prefixBytes), nodeUpdates)
	}

	// Attempt to update the stake metrics
	primaryValidators, ok := ts.cfg.Validators.GetValidators(constants.PrimaryNetworkID)
	if !ok {
		return nil
	}
	weight, _ := primaryValidators.GetWeight(ts.ctx.NodeID)
	ts.localStake.Set(float64(weight))
	ts.totalStake.Set(float64(primaryValidators.Weight()))
	return nil
}

func (ts *state) writePendingStakers() error {
	for _, tx := range ts.addedPendingStakers {
		var db database.KeyValueWriter
		switch tx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			db = ts.pendingValidatorList
		case *unsigned.AddDelegatorTx:
			db = ts.pendingDelegatorList
		case *unsigned.AddSubnetValidatorTx:
			db = ts.pendingSubnetValidatorList
		default:
			return unsigned.ErrWrongTxType
		}

		txID := tx.ID()
		if err := db.Put(txID[:], nil); err != nil {
			return fmt.Errorf("failed to write pending stakers with: %w", err)
		}
	}
	ts.addedPendingStakers = nil

	for _, tx := range ts.deletedPendingStakers {
		var db database.KeyValueDeleter
		switch tx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			db = ts.pendingValidatorList
		case *unsigned.AddDelegatorTx:
			db = ts.pendingDelegatorList
		case *unsigned.AddSubnetValidatorTx:
			db = ts.pendingSubnetValidatorList
		default:
			return unsigned.ErrWrongTxType
		}

		txID := tx.ID()
		if err := db.Delete(txID[:]); err != nil {
			return fmt.Errorf("failed to write pending stakers with: %w", err)
		}
	}
	ts.deletedPendingStakers = nil
	return nil
}

func (ts *state) writeUptimes() error {
	for nodeID := range ts.updatedUptimes {
		delete(ts.updatedUptimes, nodeID)

		uptime := ts.uptimes[nodeID]
		uptime.LastUpdated = uint64(uptime.lastUpdated.Unix())

		uptimeBytes, err := unsigned.GenCodec.Marshal(unsigned.Version, uptime)
		if err != nil {
			return fmt.Errorf("failed to write uptimes with: %w", err)
		}

		if err := ts.currentValidatorList.Put(uptime.txID[:], uptimeBytes); err != nil {
			return fmt.Errorf("failed to write uptimes with: %w", err)
		}
	}
	return nil
}

func (ts *state) writeTXs() error {
	for txID, txStatus := range ts.addedTxs {
		txID := txID

		stx := stateTx{
			Tx:     txStatus.Tx.Bytes(),
			Status: txStatus.Status,
		}

		txBytes, err := unsigned.GenCodec.Marshal(unsigned.Version, &stx)
		if err != nil {
			return fmt.Errorf("failed to write txs with: %w", err)
		}

		delete(ts.addedTxs, txID)
		ts.txCache.Put(txID, txStatus)
		if err := ts.txDB.Put(txID[:], txBytes); err != nil {
			return fmt.Errorf("failed to write txs with: %w", err)
		}
	}
	return nil
}

func (ts *state) writeRewardUTXOs() error {
	for txID, utxos := range ts.addedRewardUTXOs {
		delete(ts.addedRewardUTXOs, txID)
		ts.rewardUTXOsCache.Put(txID, utxos)
		rawTxDB := prefixdb.New(txID[:], ts.rewardUTXODB)
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

func (ts *state) writeUTXOs() error {
	for utxoID, utxo := range ts.modifiedUTXOs {
		delete(ts.modifiedUTXOs, utxoID)

		if utxo == nil {
			if err := ts.utxoState.DeleteUTXO(utxoID); err != nil {
				return fmt.Errorf("failed to write UTXOs with: %w", err)
			}
			continue
		}
		if err := ts.utxoState.PutUTXO(utxoID, utxo); err != nil {
			return fmt.Errorf("failed to write UTXOs with: %w", err)
		}
	}
	return nil
}

func (ts *state) writeSubnets() error {
	for _, subnet := range ts.addedSubnets {
		subnetID := subnet.ID()

		if err := ts.subnetDB.Put(subnetID[:], nil); err != nil {
			return fmt.Errorf("failed to write current subnets with: %w", err)
		}
	}
	ts.addedSubnets = nil
	return nil
}

func (ts *state) writeChains() error {
	for subnetID, chains := range ts.addedChains {
		for _, chain := range chains {
			chainDB := ts.getChainDB(subnetID)

			chainID := chain.ID()
			if err := chainDB.Put(chainID[:], nil); err != nil {
				return fmt.Errorf("failed to write chains with: %w", err)
			}
		}
		delete(ts.addedChains, subnetID)
	}
	return nil
}

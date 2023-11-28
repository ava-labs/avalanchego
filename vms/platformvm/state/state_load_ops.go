// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// var errNotYetImplemented = errors.New("NOT YET IMPLEMENTED")

// If [ms] isn't initialized, initializes it with [genesis].
// Then loads [ms] from disk.
func (s *state) sync(genesis []byte) error {
	shouldInit, err := s.shouldInit()
	if err != nil {
		return fmt.Errorf(
			"failed to check if the database is initialized: %w",
			err,
		)
	}

	// If the database is empty, create the platform chain anew using the
	// provided genesis state
	if shouldInit {
		if err := s.init(genesis); err != nil {
			return fmt.Errorf(
				"failed to initialize the database: %w",
				err,
			)
		}
	}

	return s.load(shouldInit)
}

// Creates a genesis from [genesisBytes] and initializes [ms] with it.
func (s *state) init(genesisBytes []byte) error {
	// Create the genesis block and save it as being accepted (We don't do
	// genesisBlock.Accept() because then it'd look for genesisBlock's
	// non-existent parent)
	genesisID := hashing.ComputeHash256Array(genesisBytes)
	genesisBlock, err := block.NewApricotCommitBlock(genesisID, 0 /*height*/)
	if err != nil {
		return err
	}

	genesisState, err := genesis.Parse(genesisBytes)
	if err != nil {
		return err
	}
	if err := s.syncGenesis(genesisBlock, genesisState); err != nil {
		return err
	}

	if err := s.doneInit(); err != nil {
		return err
	}

	return s.Commit()
}

// Load pulls data previously stored on disk that is expected to be in memory.
func (s *state) load(hasSynced bool) error {
	return utils.Err(
		s.loadMerkleMetadata(),
		s.loadCurrentStakers(),
		s.loadPendingStakers(),
		s.initValidatorSets(),

		s.logMerkleRoot(!hasSynced), // we already logged if sync has happened
	)
}

// Loads the chain time and last accepted block ID from disk
// and populates them in [ms].
func (s *state) loadMerkleMetadata() error {
	// load chain time
	chainTimeBytes, err := s.merkleDB.Get(merkleChainTimeKey)
	if err != nil {
		return err
	}
	var chainTime time.Time
	if err := chainTime.UnmarshalBinary(chainTimeBytes); err != nil {
		return err
	}
	s.latestComittedChainTime = chainTime
	s.SetTimestamp(chainTime)

	// load last accepted block
	blkIDBytes, err := s.merkleDB.Get(merkleLastAcceptedBlkIDKey)
	if err != nil {
		return err
	}
	lastAcceptedBlkID := ids.Empty
	copy(lastAcceptedBlkID[:], blkIDBytes)
	s.latestCommittedLastAcceptedBlkID = lastAcceptedBlkID
	s.SetLastAccepted(lastAcceptedBlkID)

	// We don't need to load supplies. Unlike chain time and last block ID,
	// which have the persisted* attribute, we signify that a supply hasn't
	// been modified by making it nil.
	return nil
}

// Loads current stakes from disk and populates them in [ms].
func (s *state) loadCurrentStakers() error {
	// TODO ABENEGIA: Check missing metadata
	s.currentStakers = newBaseStakers()

	prefix := make([]byte, len(currentStakersSectionPrefix))
	copy(prefix, currentStakersSectionPrefix)

	iter := s.merkleDB.NewIteratorWithPrefix(prefix)
	defer iter.Release()
	for iter.Next() {
		data := &stakersData{}
		if _, err := txs.GenesisCodec.Unmarshal(iter.Value(), data); err != nil {
			return fmt.Errorf("failed to deserialize current stakers data: %w", err)
		}

		tx, err := txs.Parse(txs.GenesisCodec, data.TxBytes)
		if err != nil {
			return fmt.Errorf("failed to parsing current stakerTx: %w", err)
		}
		stakerTx, ok := tx.Unsigned.(txs.Staker)
		if !ok {
			return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
		}

		staker, err := NewCurrentStaker(tx.ID(), stakerTx, data.PotentialReward)
		if err != nil {
			return err
		}
		if staker.Priority.IsValidator() {
			// TODO: why not PutValidator/PutDelegator??
			validator := s.currentStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
			validator.validator = staker
			s.currentStakers.stakers.ReplaceOrInsert(staker)
		} else {
			validator := s.currentStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
			if validator.delegators == nil {
				validator.delegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
			}
			validator.delegators.ReplaceOrInsert(staker)
			s.currentStakers.stakers.ReplaceOrInsert(staker)
		}
	}
	return iter.Error()
}

func (s *state) loadPendingStakers() error {
	// TODO ABENEGIA: Check missing metadata
	s.pendingStakers = newBaseStakers()

	prefix := make([]byte, len(pendingStakersSectionPrefix))
	copy(prefix, pendingStakersSectionPrefix)

	iter := s.merkleDB.NewIteratorWithPrefix(prefix)
	defer iter.Release()
	for iter.Next() {
		data := &stakersData{}
		if _, err := txs.GenesisCodec.Unmarshal(iter.Value(), data); err != nil {
			return fmt.Errorf("failed to deserialize pending stakers data: %w", err)
		}

		tx, err := txs.Parse(txs.GenesisCodec, data.TxBytes)
		if err != nil {
			return fmt.Errorf("failed to parsing pending stakerTx: %w", err)
		}
		stakerTx, ok := tx.Unsigned.(txs.Staker)
		if !ok {
			return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
		}

		staker, err := NewPendingStaker(tx.ID(), stakerTx)
		if err != nil {
			return err
		}
		if staker.Priority.IsValidator() {
			validator := s.pendingStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
			validator.validator = staker
			s.pendingStakers.stakers.ReplaceOrInsert(staker)
		} else {
			validator := s.pendingStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
			if validator.delegators == nil {
				validator.delegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
			}
			validator.delegators.ReplaceOrInsert(staker)
			s.pendingStakers.stakers.ReplaceOrInsert(staker)
		}
	}
	return iter.Error()
}

// Invariant: initValidatorSets requires loadCurrentValidators to have already
// been called.
func (s *state) initValidatorSets() error {
	for subnetID, validators := range s.currentStakers.validators {
		if s.validators.Count(subnetID) != 0 {
			// Enforce the invariant that the validator set is empty here.
			return fmt.Errorf("%w: %s", errValidatorSetAlreadyPopulated, subnetID)
		}

		for nodeID, validator := range validators {
			validatorStaker := validator.validator
			if err := s.validators.AddStaker(subnetID, nodeID, validatorStaker.PublicKey, validatorStaker.TxID, validatorStaker.Weight); err != nil {
				return err
			}

			delegatorIterator := NewTreeIterator(validator.delegators)
			for delegatorIterator.Next() {
				delegatorStaker := delegatorIterator.Value()
				if err := s.validators.AddWeight(subnetID, nodeID, delegatorStaker.Weight); err != nil {
					delegatorIterator.Release()
					return err
				}
			}
			delegatorIterator.Release()
		}
	}

	s.metrics.SetLocalStake(s.validators.GetWeight(constants.PrimaryNetworkID, s.ctx.NodeID))
	totalWeight, err := s.validators.TotalWeight(constants.PrimaryNetworkID)
	if err != nil {
		return fmt.Errorf("failed to get total weight of primary network validators: %w", err)
	}
	s.metrics.SetTotalStake(totalWeight)
	return nil
}

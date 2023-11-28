// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
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

// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const (
	pending uint8 = iota + 1
	current
)

// Check that CRUD operations over current stakers behaves uniformly for diffs and base state
func TestCurrentValidatorsCRUD(t *testing.T) {
	subnetID := ids.GenerateTestID()

	tests := []struct {
		name         string
		stakersStore func() (Stakers, error)
	}{
		{
			name: "base state",
			stakersStore: func() (Stakers, error) {
				baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
				baseDB := versiondb.New(baseDBManager.Current().Database)
				return buildChainState(baseDB, nil)
			},
		},
		{
			name: "diff",
			stakersStore: func() (Stakers, error) {
				diff, err := buildDiffOnTopOfBaseState([]ids.ID{subnetID})
				return diff, err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			store, err := tt.stakersStore()
			require.NoError(err)

			var (
				now             = time.Now()
				stakingDuration = 365 * 24 * time.Hour
				validator       = &Staker{
					TxID:     ids.GenerateTestID(),
					NodeID:   ids.GenerateTestNodeID(),
					SubnetID: subnetID,
					Weight:   uint64(2023),

					StartTime: now,
					NextTime:  now.Add(stakingDuration),
					EndTime:   now.Add(stakingDuration),
					Priority:  txs.PrimaryNetworkValidatorCurrentPriority,
				}
			)

			{
				// no validators before insertion

				// point query
				_, err = store.GetCurrentValidator(validator.SubnetID, validator.NodeID)
				require.ErrorIs(err, database.ErrNotFound)

				// range query
				expectedResult := []*Staker{}
				require.NoError(checkStakersContent(store, current, expectedResult))
			}

			{
				// it's fine deleting unknown validator
				store.DeleteCurrentValidator(validator)

				// point query
				_, err = store.GetCurrentValidator(validator.SubnetID, validator.NodeID)
				require.ErrorIs(err, database.ErrNotFound)

				// range query
				expectedResult := []*Staker{}
				require.NoError(checkStakersContent(store, current, expectedResult))
			}

			{
				// insert the validator and show it can be found
				store.PutCurrentValidator(validator)

				// point query
				retrievedStaker, err := store.GetCurrentValidator(validator.SubnetID, validator.NodeID)
				require.NoError(err)
				require.Equal(validator, retrievedStaker)

				// range query
				expectedResult := []*Staker{validator}
				require.NoError(checkStakersContent(store, current, expectedResult))
			}

			{
				// delete the validators and show it's not found anymore
				store.DeleteCurrentValidator(validator)

				// point query
				_, err = store.GetCurrentValidator(validator.SubnetID, validator.NodeID)
				require.ErrorIs(err, database.ErrNotFound)

				// range query
				expectedResult := []*Staker{}
				require.NoError(checkStakersContent(store, current, expectedResult))
			}
		})
	}
}

func TestCurrentDelegatorsCRUD(t *testing.T) {
	subnetID := ids.GenerateTestID()

	tests := []struct {
		name         string
		stakersStore func() (Stakers, error)
	}{
		{
			name: "base state",
			stakersStore: func() (Stakers, error) {
				baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
				baseDB := versiondb.New(baseDBManager.Current().Database)
				return buildChainState(baseDB, nil)
			},
		},
		{
			name: "diff",
			stakersStore: func() (Stakers, error) {
				diff, err := buildDiffOnTopOfBaseState([]ids.ID{subnetID})
				return diff, err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			store, err := tt.stakersStore()
			require.NoError(err)

			var (
				now               = time.Now()
				delegatorDuration = 30 * 24 * time.Hour

				delegator = &Staker{
					TxID:     ids.GenerateTestID(),
					NodeID:   ids.GenerateTestNodeID(),
					SubnetID: subnetID,
					Weight:   uint64(2023),

					StartTime: now,
					NextTime:  now.Add(delegatorDuration),
					EndTime:   now.Add(delegatorDuration),
					Priority:  txs.PrimaryNetworkDelegatorCurrentPriority,
				}
			)

			{
				// no delegator before insertion

				// query
				expectedResult := []*Staker{}
				err := checkDelegatorsContent(store, current, delegator.SubnetID, delegator.NodeID, expectedResult)
				require.NoError(err)

				// range query
				require.NoError(checkStakersContent(store, current, expectedResult))
			}

			{
				// it's fine deleting unknown delegator
				store.DeleteCurrentDelegator(delegator)

				// query
				expectedResult := []*Staker{}
				err := checkDelegatorsContent(store, current, delegator.SubnetID, delegator.NodeID, expectedResult)
				require.NoError(err)

				// range query
				require.NoError(checkStakersContent(store, current, expectedResult))
			}

			{
				// insert the delegator and show it can be found
				store.PutCurrentDelegator(delegator)

				// query
				expectedResult := []*Staker{delegator}
				err := checkDelegatorsContent(store, current, delegator.SubnetID, delegator.NodeID, expectedResult)
				require.NoError(err)

				// range query
				require.NoError(checkStakersContent(store, current, expectedResult))
			}

			{
				// delete the delegator and show it's not found anymore
				store.DeleteCurrentDelegator(delegator)

				// query
				expectedResult := []*Staker{}
				err := checkDelegatorsContent(store, current, delegator.SubnetID, delegator.NodeID, expectedResult)
				require.NoError(err)

				// range query
				require.NoError(checkStakersContent(store, current, expectedResult))
			}
		})
	}
}

func TestPendingValidatorsCRUD(t *testing.T) {
	subnetID := ids.GenerateTestID()

	tests := []struct {
		name         string
		stakersStore func() (Stakers, error)
	}{
		{
			name: "base state",
			stakersStore: func() (Stakers, error) {
				baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
				baseDB := versiondb.New(baseDBManager.Current().Database)
				return buildChainState(baseDB, nil)
			},
		},
		{
			name: "diff",
			stakersStore: func() (Stakers, error) {
				diff, err := buildDiffOnTopOfBaseState([]ids.ID{subnetID})
				return diff, err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			store, err := tt.stakersStore()
			require.NoError(err)

			var (
				now             = time.Now()
				stakingDuration = 365 * 24 * time.Hour
				staker          = &Staker{
					TxID:     ids.GenerateTestID(),
					NodeID:   ids.GenerateTestNodeID(),
					SubnetID: subnetID,
					Weight:   uint64(2023),

					StartTime: now,
					NextTime:  now.Add(stakingDuration),
					EndTime:   now.Add(stakingDuration),
					Priority:  txs.PrimaryNetworkValidatorPendingPriority,
				}
			)

			{
				// no staker before insertion

				// point query
				_, err = store.GetPendingValidator(staker.SubnetID, staker.NodeID)
				require.ErrorIs(err, database.ErrNotFound)

				// range query
				expectedResult := []*Staker{}
				require.NoError(checkStakersContent(store, pending, expectedResult))
			}

			{
				// it's fine deleting unknown validator
				store.DeletePendingValidator(staker)

				// point query
				_, err = store.GetPendingValidator(staker.SubnetID, staker.NodeID)
				require.ErrorIs(err, database.ErrNotFound)

				// range query
				expectedResult := []*Staker{}
				require.NoError(checkStakersContent(store, pending, expectedResult))
			}

			{
				// insert the staker and show it can be found
				store.PutPendingValidator(staker)

				// point query
				retrievedStaker, err := store.GetPendingValidator(staker.SubnetID, staker.NodeID)
				require.NoError(err)
				require.Equal(staker, retrievedStaker)

				// range query
				expectedResult := []*Staker{staker}
				require.NoError(checkStakersContent(store, pending, expectedResult))
			}

			{
				// delete the staker and show it's not found anymore
				store.DeletePendingValidator(staker)

				// point query
				_, err = store.GetPendingValidator(staker.SubnetID, staker.NodeID)
				require.ErrorIs(err, database.ErrNotFound)

				// range query
				expectedResult := []*Staker{}
				require.NoError(checkStakersContent(store, pending, expectedResult))
			}
		})
	}
}

func TestPendingDelegatorsCRUD(t *testing.T) {
	subnetID := ids.GenerateTestID()

	tests := []struct {
		name         string
		stakersStore func() (Stakers, error)
	}{
		{
			name: "base state",
			stakersStore: func() (Stakers, error) {
				baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
				baseDB := versiondb.New(baseDBManager.Current().Database)
				return buildChainState(baseDB, nil)
			},
		},
		{
			name: "diff",
			stakersStore: func() (Stakers, error) {
				diff, err := buildDiffOnTopOfBaseState([]ids.ID{subnetID})
				return diff, err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			store, err := tt.stakersStore()
			require.NoError(err)

			var (
				now               = time.Now()
				delegatorDuration = 30 * 24 * time.Hour

				delegator = &Staker{
					TxID:     ids.GenerateTestID(),
					NodeID:   ids.GenerateTestNodeID(),
					SubnetID: subnetID,
					Weight:   uint64(2023),

					StartTime: now,
					NextTime:  now.Add(delegatorDuration),
					EndTime:   now.Add(delegatorDuration),
					Priority:  txs.PrimaryNetworkDelegatorBanffPendingPriority,
				}
			)

			{
				// no delegator before insertion

				// query
				expectedResult := []*Staker{}
				err := checkDelegatorsContent(store, pending, delegator.SubnetID, delegator.NodeID, expectedResult)
				require.NoError(err)

				// range query
				require.NoError(checkStakersContent(store, pending, expectedResult))
			}

			{
				// it's fine deleting unknown delegator
				store.DeletePendingDelegator(delegator)

				// query
				expectedResult := []*Staker{}
				err := checkDelegatorsContent(store, pending, delegator.SubnetID, delegator.NodeID, expectedResult)
				require.NoError(err)

				// range query
				require.NoError(checkStakersContent(store, pending, expectedResult))
			}

			{
				// insert the delegator and show it can be found
				store.PutPendingDelegator(delegator)

				// query
				expectedResult := []*Staker{delegator}
				err := checkDelegatorsContent(store, pending, delegator.SubnetID, delegator.NodeID, expectedResult)
				require.NoError(err)

				// range query
				require.NoError(checkStakersContent(store, pending, expectedResult))
			}

			{
				// delete the delegator and show it's not found anymore
				store.DeletePendingDelegator(delegator)

				// query
				expectedResult := []*Staker{}
				err := checkDelegatorsContent(store, pending, delegator.SubnetID, delegator.NodeID, expectedResult)
				require.NoError(err)

				// range query
				require.NoError(checkStakersContent(store, pending, expectedResult))
			}
		})
	}
}

// [checkStakersContent] verifies whether store contains exactly the stakers specified in the list.
// stakers order does not matter. stakers slice gets consumed while checking.
func checkStakersContent(store Stakers, validatorsType uint8, expectedValidators []*Staker) error {
	var (
		it  StakerIterator
		err error
	)

	switch validatorsType {
	case current:
		it, err = store.GetCurrentStakerIterator()
	case pending:
		it, err = store.GetPendingStakerIterator()
	default:
		return errors.New("Unhandled stakers status")
	}
	if err != nil {
		return fmt.Errorf("unexpected failure in staker iterator creation, error %w", err)
	}
	defer it.Release()

	if len(expectedValidators) == 0 {
		if it.Next() {
			return fmt.Errorf("expected empty iterator, got at least element %v", it.Value())
		}
		return nil
	}

	for it.Next() {
		var (
			staker = it.Value()
			found  = false

			retrievedStakerIdx = 0
		)

		for idx, s := range expectedValidators {
			if reflect.DeepEqual(staker, s) {
				retrievedStakerIdx = idx
				found = true
			}
		}
		if !found {
			return fmt.Errorf("found extra staker %v", staker)
		}
		expectedValidators[retrievedStakerIdx] = expectedValidators[len(expectedValidators)-1] // order does not matter
		expectedValidators = expectedValidators[:len(expectedValidators)-1]
	}

	if len(expectedValidators) != 0 {
		return errors.New("missing stakers")
	}
	return nil
}

func checkDelegatorsContent(store Stakers, delegatorsType uint8, subnetID ids.ID, nodeID ids.NodeID, expectedDelegators []*Staker) error {
	var (
		it  StakerIterator
		err error
	)

	switch delegatorsType {
	case current:
		it, err = store.GetCurrentDelegatorIterator(subnetID, nodeID)
	case pending:
		it, err = store.GetPendingDelegatorIterator(subnetID, nodeID)
	default:
		return errors.New("Unhandled delegators status")
	}
	if err != nil {
		return fmt.Errorf("unexpected failure in delegators iterator creation, error %w", err)
	}
	defer it.Release()

	if len(expectedDelegators) == 0 {
		if it.Next() {
			return fmt.Errorf("expected empty iterator, got at least element %v", it.Value())
		}
		return nil
	}

	for it.Next() {
		var (
			delegator = it.Value()
			found     = false

			retrievedStakerIdx = 0
		)

		for idx, s := range expectedDelegators {
			if reflect.DeepEqual(delegator, s) {
				retrievedStakerIdx = idx
				found = true
			}
		}
		if !found {
			return fmt.Errorf("found extra delegator %v", delegator)
		}
		expectedDelegators[retrievedStakerIdx] = expectedDelegators[len(expectedDelegators)-1] // order does not matter
		expectedDelegators = expectedDelegators[:len(expectedDelegators)-1]
	}

	if len(expectedDelegators) != 0 {
		return errors.New("missing delegators")
	}
	return nil
}

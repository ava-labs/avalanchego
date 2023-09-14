// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const (
	pending stakerStatus = 0
	current stakerStatus = 1
)

// TestGeneralStakerContainersProperties checks that State and Diff conform our stakersStorageModel.
// TestGeneralStakerContainersProperties tests State and Diff in isolation, over simple operations.
// TestStateAndDiffComparisonToStorageModel carries a more involved verification over a production-like
// mix of State and Diffs.
func TestGeneralStakerContainersProperties(t *testing.T) {
	storeCreators := map[string]func() (Stakers, error){
		"base state": func() (Stakers, error) {
			baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
			baseDB := versiondb.New(baseDBManager.Current().Database)
			return buildChainState(baseDB, nil)
		},
		"diff": func() (Stakers, error) {
			diff, _, err := buildDiffOnTopOfBaseState(nil)
			return diff, err
		},
		"storage model": func() (Stakers, error) { //nolint:golint,unparam
			return newStakersStorageModel(), nil
		},
	}

	for storeType, storeCreatorF := range storeCreators {
		t.Run(storeType, func(t *testing.T) {
			properties := generalStakerContainersProperties(storeCreatorF)
			properties.TestingRun(t)
		})
	}
}

func generalStakerContainersProperties(storeCreatorF func() (Stakers, error)) *gopter.Properties {
	properties := gopter.NewProperties(nil)

	ctx := buildStateCtx()

	properties.Property("add, delete and query current validators", prop.ForAll(
		func(nonInitTx *txs.Tx) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			signedTx, err := txs.NewSigned(nonInitTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedTx.Unsigned.(txs.StakerTx)
			staker, err := NewCurrentStaker(signedTx.ID(), stakerTx, uint64(100))
			if err != nil {
				return err.Error()
			}

			// no staker before insertion
			_, err = store.GetCurrentValidator(staker.SubnetID, staker.NodeID)
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}
			err = checkStakersContent(store, []*Staker{}, current)
			if err != nil {
				return err.Error()
			}

			// it's fine deleting unknown validator
			store.DeleteCurrentValidator(staker)
			_, err = store.GetCurrentValidator(staker.SubnetID, staker.NodeID)
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}
			err = checkStakersContent(store, []*Staker{}, current)
			if err != nil {
				return err.Error()
			}

			// insert the staker and show it can be found
			store.PutCurrentValidator(staker)
			retrievedStaker, err := store.GetCurrentValidator(staker.SubnetID, staker.NodeID)
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(staker, retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", staker, retrievedStaker)
			}
			err = checkStakersContent(store, []*Staker{staker}, current)
			if err != nil {
				return err.Error()
			}

			// delete the staker and show it's not found anymore
			store.DeleteCurrentValidator(staker)
			_, err = store.GetCurrentValidator(staker.SubnetID, staker.NodeID)
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}
			err = checkStakersContent(store, []*Staker{}, current)
			if err != nil {
				return err.Error()
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionedValidator, &constants.PrimaryNetworkID, nil, math.MaxUint64),
	))

	properties.Property("add, delete and query pending validators", prop.ForAll(
		func(nonInitTx *txs.Tx) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			signedTx, err := txs.NewSigned(nonInitTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			staker, err := NewPendingStaker(signedTx.ID(), signedTx.Unsigned.(txs.StakerTx))
			if err != nil {
				return err.Error()
			}

			// no staker before insertion
			_, err = store.GetPendingValidator(staker.SubnetID, staker.NodeID)
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}
			err = checkStakersContent(store, []*Staker{}, pending)
			if err != nil {
				return err.Error()
			}

			// it's fine deleting unknown validator
			store.DeletePendingValidator(staker)
			_, err = store.GetPendingValidator(staker.SubnetID, staker.NodeID)
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}
			err = checkStakersContent(store, []*Staker{}, pending)
			if err != nil {
				return err.Error()
			}

			// insert the staker and show it can be found
			store.PutPendingValidator(staker)
			retrievedStaker, err := store.GetPendingValidator(staker.SubnetID, staker.NodeID)
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(staker, retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", staker, retrievedStaker)
			}
			err = checkStakersContent(store, []*Staker{staker}, pending)
			if err != nil {
				return err.Error()
			}

			// delete the staker and show it's found anymore
			store.DeletePendingValidator(staker)
			_, err = store.GetPendingValidator(staker.SubnetID, staker.NodeID)
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}
			err = checkStakersContent(store, []*Staker{}, pending)
			if err != nil {
				return err.Error()
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionedValidator, &constants.PrimaryNetworkID, nil, math.MaxUint64),
	))

	var (
		subnetID = ids.GenerateTestID()
		nodeID   = ids.GenerateTestNodeID()
	)
	properties.Property("add, delete and query current delegators", prop.ForAll(
		func(nonInitValTx *txs.Tx, nonInitDelTxs []*txs.Tx) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			signedValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			val, err := NewCurrentStaker(signedValTx.ID(), signedValTx.Unsigned.(txs.StakerTx), uint64(1000))
			if err != nil {
				return err.Error()
			}

			dels := make([]*Staker, 0, len(nonInitDelTxs))
			for _, nonInitDelTx := range nonInitDelTxs {
				signedDelTx, err := txs.NewSigned(nonInitDelTx.Unsigned, txs.Codec, nil)
				if err != nil {
					panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
				}

				del, err := NewCurrentStaker(signedDelTx.ID(), signedDelTx.Unsigned.(txs.StakerTx), uint64(1000))
				if err != nil {
					return err.Error()
				}

				dels = append(dels, del)
			}

			// store validator
			store.PutCurrentValidator(val)
			retrievedValidator, err := store.GetCurrentValidator(val.SubnetID, val.NodeID)
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(val, retrievedValidator) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &val, retrievedValidator)
			}
			err = checkStakersContent(store, []*Staker{val}, current)
			if err != nil {
				return err.Error()
			}

			// store delegators
			for _, del := range dels {
				cpy := *del

				// it's fine deleting unknown delegator
				store.DeleteCurrentDelegator(&cpy)

				// finally store the delegator
				store.PutCurrentDelegator(&cpy)
			}

			// check no missing delegators by subnetID, nodeID
			for _, del := range dels {
				found := false
				delIt, err := store.GetCurrentDelegatorIterator(subnetID, nodeID)
				if err != nil {
					return fmt.Sprintf("unexpected failure in current delegators iterator creation, error %v", err)
				}
				for delIt.Next() {
					if reflect.DeepEqual(delIt.Value(), del) {
						found = true
						break
					}
				}
				delIt.Release()

				if !found {
					return fmt.Sprintf("missing delegator %v", del)
				}
			}

			// check no extra delegator by subnetID, nodeID
			delIt, err := store.GetCurrentDelegatorIterator(subnetID, nodeID)
			if err != nil {
				return fmt.Sprintf("unexpected failure in current delegators iterator creation, error %v", err)
			}
			for delIt.Next() {
				found := false
				for _, del := range dels {
					if reflect.DeepEqual(delIt.Value(), del) {
						found = true
						break
					}
				}
				if !found {
					return fmt.Sprintf("found extra delegator %v", delIt.Value())
				}
			}
			delIt.Release()

			// check no missing delegators in the whole staker set
			stakersSet := dels
			stakersSet = append(stakersSet, val)
			err = checkStakersContent(store, stakersSet, current)
			if err != nil {
				return err.Error()
			}

			// delete delegators
			for _, del := range dels {
				cpy := *del
				store.DeleteCurrentDelegator(&cpy)

				// check deleted delegator is not there anymore
				delIt, err := store.GetCurrentDelegatorIterator(subnetID, nodeID)
				if err != nil {
					return fmt.Sprintf("unexpected failure in current delegators iterator creation, error %v", err)
				}

				found := false
				for delIt.Next() {
					if reflect.DeepEqual(delIt.Value(), del) {
						found = true
						break
					}
				}
				delIt.Release()
				if found {
					return fmt.Sprintf("found deleted delegator %v", del)
				}
			}

			return ""
		},
		stakerTxGenerator(ctx,
			permissionlessValidator,
			&subnetID,
			&nodeID,
			math.MaxUint64,
		),
		gen.SliceOfN(10,
			stakerTxGenerator(ctx,
				permissionlessDelegator,
				&subnetID,
				&nodeID,
				1000,
			),
		),
	))

	properties.Property("add, delete and query pending delegators", prop.ForAll(
		func(nonInitValTx *txs.Tx, nonInitDelTxs []*txs.Tx) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			signedValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			val, err := NewCurrentStaker(signedValTx.ID(), signedValTx.Unsigned.(txs.StakerTx), uint64(1000))
			if err != nil {
				return err.Error()
			}

			dels := make([]*Staker, 0, len(nonInitDelTxs))
			for _, nonInitDelTx := range nonInitDelTxs {
				signedDelTx, err := txs.NewSigned(nonInitDelTx.Unsigned, txs.Codec, nil)
				if err != nil {
					panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
				}

				del, err := NewCurrentStaker(signedDelTx.ID(), signedDelTx.Unsigned.(txs.StakerTx), uint64(1000))
				if err != nil {
					return err.Error()
				}

				dels = append(dels, del)
			}

			// store validator
			store.PutCurrentValidator(val)
			retrievedValidator, err := store.GetCurrentValidator(val.SubnetID, val.NodeID)
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(val, retrievedValidator) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &val, retrievedValidator)
			}

			err = checkStakersContent(store, []*Staker{val}, current)
			if err != nil {
				return err.Error()
			}

			// store delegators
			for _, del := range dels {
				cpy := *del

				// it's fine deleting unknown delegator
				store.DeletePendingDelegator(&cpy)

				// finally store the delegator
				store.PutPendingDelegator(&cpy)
			}

			// check no missing delegators by subnetID, nodeID
			for _, del := range dels {
				found := false
				delIt, err := store.GetPendingDelegatorIterator(subnetID, nodeID)
				if err != nil {
					return fmt.Sprintf("unexpected failure in pending delegators iterator creation, error %v", err)
				}
				for delIt.Next() {
					if reflect.DeepEqual(delIt.Value(), del) {
						found = true
						break
					}
				}
				delIt.Release()

				if !found {
					return fmt.Sprintf("missing delegator %v", del)
				}
			}

			// check no extra delegators by subnetID, nodeID
			delIt, err := store.GetPendingDelegatorIterator(subnetID, nodeID)
			if err != nil {
				return fmt.Sprintf("unexpected failure in pending delegators iterator creation, error %v", err)
			}
			for delIt.Next() {
				found := false
				for _, del := range dels {
					if reflect.DeepEqual(delIt.Value(), del) {
						found = true
						break
					}
				}
				if !found {
					return fmt.Sprintf("found extra delegator %v", delIt.Value())
				}
			}
			delIt.Release()

			// check no missing delegators in the whole staker set
			err = checkStakersContent(store, dels, pending)
			if err != nil {
				return err.Error()
			}

			// delete delegators
			for _, del := range dels {
				cpy := *del
				store.DeletePendingDelegator(&cpy)

				// check deleted delegator is not there anymore
				delIt, err := store.GetPendingDelegatorIterator(subnetID, nodeID)
				if err != nil {
					return fmt.Sprintf("unexpected failure in pending delegators iterator creation, error %v", err)
				}

				found := false
				for delIt.Next() {
					if reflect.DeepEqual(delIt.Value(), del) {
						found = true
						break
					}
				}
				delIt.Release()
				if found {
					return fmt.Sprintf("found deleted delegator %v", del)
				}
			}

			return ""
		},
		stakerTxGenerator(ctx,
			permissionlessValidator,
			&subnetID,
			&nodeID,
			math.MaxUint64,
		),
		gen.SliceOfN(10,
			stakerTxGenerator(ctx,
				permissionlessDelegator,
				&subnetID,
				&nodeID,
				1000,
			),
		),
	))

	return properties
}

func buildDiffOnTopOfBaseState(trackedSubnets []ids.ID) (Diff, State, error) {
	baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
	baseDB := versiondb.New(baseDBManager.Current().Database)
	baseState, err := buildChainState(baseDB, trackedSubnets)
	if err != nil {
		return nil, nil, fmt.Errorf("unexpected error while creating chain base state, err %w", err)
	}

	genesisID := baseState.GetLastAccepted()
	versions := &versionsHolder{
		baseState: baseState,
	}
	diff, err := NewDiff(genesisID, versions)
	if err != nil {
		return nil, nil, fmt.Errorf("unexpected error while creating diff, err %w", err)
	}
	return diff, baseState, nil
}

// [checkStakersContent] verifies whether store contains exactly the stakers specified in the list.
// stakers order does not matter. stakers slice gets consumed while checking.
func checkStakersContent(store Stakers, stakers []*Staker, stakersType stakerStatus) error {
	var (
		it  StakerIterator
		err error
	)

	switch stakersType {
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

	if len(stakers) == 0 {
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

		for idx, s := range stakers {
			if reflect.DeepEqual(staker, s) {
				retrievedStakerIdx = idx
				found = true
			}
		}
		if !found {
			return fmt.Errorf("found extra staker %v", staker)
		}
		stakers[retrievedStakerIdx] = stakers[len(stakers)-1] // order does not matter
		stakers = stakers[:len(stakers)-1]
	}

	if len(stakers) != 0 {
		return errors.New("missing stakers")
	}
	return nil
}

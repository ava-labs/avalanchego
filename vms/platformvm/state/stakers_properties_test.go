// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

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

var (
	errNonEmptyIteratorExpected          = errors.New("expected non-empty iterator, got no elements")
	errMissingValidatotFromValidatorSet  = errors.New("staker cannot be found in validator set")
	errUnexpectedValidatotInValidatorSet = errors.New("unexpected staker found in validator set")
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
	startTime := time.Now().Truncate(time.Second)

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
			staker, err := NewCurrentStaker(signedTx.ID(), stakerTx, startTime, uint64(100))
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

	properties.Property("update current validators", prop.ForAll(
		func(nonInitTx *txs.Tx) string {
			// insert stakers first, then update StartTime/EndTime and update the staker
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			signedTx, err := txs.NewSigned(nonInitTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedTx.Unsigned.(txs.StakerTx)
			staker, err := NewCurrentStaker(signedTx.ID(), stakerTx, startTime, uint64(100))
			if err != nil {
				return err.Error()
			}

			store.PutCurrentValidator(staker)
			retrievedStaker, err := store.GetCurrentValidator(staker.SubnetID, staker.NodeID)
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(staker, retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", staker, retrievedStaker)
			}

			currIT, err := store.GetCurrentStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if !currIT.Next() {
				return errNonEmptyIteratorExpected.Error()
			}
			if !reflect.DeepEqual(currIT.Value(), retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", staker, retrievedStaker)
			}
			currIT.Release()

			// update staker times as expected. We copy the updated staker
			// to avoid in-place modification of stakers already stored in store,
			// as it must be done in prod code.
			updatedStaker := *staker
			ShiftStakerAheadInPlace(&updatedStaker, updatedStaker.EndTime)

			err = store.UpdateCurrentValidator(&updatedStaker)
			if err != nil {
				return fmt.Sprintf("expected no error in updating, got %v", err)
			}

			// show that queries return updated staker, not original one
			retrievedStaker, err = store.GetCurrentValidator(updatedStaker.SubnetID, updatedStaker.NodeID)
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(&updatedStaker, retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &updatedStaker, retrievedStaker)
			}

			currIT, err = store.GetCurrentStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if !currIT.Next() {
				return errNonEmptyIteratorExpected.Error()
			}
			if !reflect.DeepEqual(currIT.Value(), retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", currIT.Value(), retrievedStaker)
			}
			currIT.Release()
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

			staker, err := NewPendingStaker(signedTx.ID(), signedTx.Unsigned.(txs.PreContinuousStakingStaker))
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

			val, err := NewCurrentStaker(signedValTx.ID(), signedValTx.Unsigned.(txs.StakerTx), startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			dels := make([]*Staker, 0, len(nonInitDelTxs))
			for _, nonInitDelTx := range nonInitDelTxs {
				signedDelTx, err := txs.NewSigned(nonInitDelTx.Unsigned, txs.Codec, nil)
				if err != nil {
					panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
				}

				del, err := NewCurrentStaker(signedDelTx.ID(), signedDelTx.Unsigned.(txs.StakerTx), startTime, uint64(1000))
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

	properties.Property("update current delegator", prop.ForAll(
		func(nonInitDelTxs []*txs.Tx) string {
			// insert stakers first, then update StartTime/EndTime and update the staker
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			// store delegators
			dels := make([]*Staker, 0, len(nonInitDelTxs))
			for _, nonInitDelTx := range nonInitDelTxs {
				signedDelTx, err := txs.NewSigned(nonInitDelTx.Unsigned, txs.Codec, nil)
				if err != nil {
					panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
				}
				stakerTx := signedDelTx.Unsigned.(txs.StakerTx)
				del, err := NewCurrentStaker(signedDelTx.ID(), stakerTx, startTime, uint64(1000))
				if err != nil {
					return err.Error()
				}

				dels = append(dels, del)
				store.PutCurrentDelegator(del)
			}

			// update delegators
			for _, del := range dels {
				// update staker times as expected. We copy the updated staker
				// to avoid in-place modification of stakers already stored in store,
				// as it must be done in prod code.
				updatedStaker := *del
				ShiftStakerAheadInPlace(&updatedStaker, updatedStaker.EndTime)

				err = store.UpdateCurrentDelegator(&updatedStaker)
				if err != nil {
					return fmt.Sprintf("expected no error in updating, got %v", err)
				}

				// check query returns updated staker - version 1
				delIt, err := store.GetCurrentDelegatorIterator(updatedStaker.SubnetID, updatedStaker.NodeID)
				if err != nil {
					return fmt.Sprintf("expected no error, got %v", err)
				}

				found := false
				for delIt.Next() {
					del := delIt.Value()
					if del.TxID != updatedStaker.TxID {
						continue
					}
					found = true
					if !reflect.DeepEqual(&updatedStaker, del) {
						return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &updatedStaker, del)
					}
					break
				}
				if !found {
					return fmt.Sprintf("could not find updated staker %v", &updatedStaker)
				}
				delIt.Release()

				// check query returns updated staker - version 2
				stakerIt, err := store.GetCurrentStakerIterator()
				if err != nil {
					return fmt.Sprintf("expected no error, got %v", err)
				}
				found = false
				for stakerIt.Next() {
					del := stakerIt.Value()
					if del.TxID != updatedStaker.TxID {
						continue
					}
					found = true
					if !reflect.DeepEqual(&updatedStaker, del) {
						return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &updatedStaker, del)
					}
					break
				}
				if !found {
					return fmt.Sprintf("could not find updated staker %v", &updatedStaker)
				}
				stakerIt.Release()
			}
			return ""
		},
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

			val, err := NewCurrentStaker(signedValTx.ID(), signedValTx.Unsigned.(txs.StakerTx), startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			dels := make([]*Staker, 0, len(nonInitDelTxs))
			for _, nonInitDelTx := range nonInitDelTxs {
				signedDelTx, err := txs.NewSigned(nonInitDelTx.Unsigned, txs.Codec, nil)
				if err != nil {
					panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
				}

				del, err := NewCurrentStaker(signedDelTx.ID(), signedDelTx.Unsigned.(txs.StakerTx), startTime, uint64(1000))
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

// TestStateStakersProperties verifies properties specific to State, but not to Diff
func TestStateStakersProperties(t *testing.T) {
	properties := gopter.NewProperties(nil)
	ctx := buildStateCtx()
	startTime := time.Now().Truncate(time.Second)

	properties.Property("cannot update unknown validator", prop.ForAll(
		func(nonInitValTx *txs.Tx) string {
			baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
			baseDB := versiondb.New(baseDBManager.Current().Database)
			baseState, err := buildChainState(baseDB, nil)
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			signedValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedValTx.Unsigned.(txs.StakerTx)
			val, err := NewCurrentStaker(signedValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			err = baseState.UpdateCurrentValidator(val)
			if !errors.Is(err, ErrUpdatingUnknownOrDeletedStaker) {
				return "unexpected update of unknown validator"
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessValidator, nil, nil, math.MaxUint64),
	))

	properties.Property("cannot update deleted validator", prop.ForAll(
		func(nonInitValTx *txs.Tx) string {
			baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
			baseDB := versiondb.New(baseDBManager.Current().Database)
			baseState, err := buildChainState(baseDB, nil)
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			signedValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedValTx.Unsigned.(txs.StakerTx)
			val, err := NewCurrentStaker(signedValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			baseState.PutCurrentValidator(val)
			baseState.DeleteCurrentValidator(val)
			err = baseState.UpdateCurrentValidator(val)
			if !errors.Is(err, ErrUpdatingUnknownOrDeletedStaker) {
				return "unexpected update of unknown validator"
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessValidator, nil, nil, math.MaxUint64),
	))

	properties.Property("cannot update delegator from unknown subnetID/nodeID", prop.ForAll(
		func(nonInitValTx *txs.Tx) string {
			baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
			baseDB := versiondb.New(baseDBManager.Current().Database)
			baseState, err := buildChainState(baseDB, nil)
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			signedValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedValTx.Unsigned.(txs.StakerTx)
			val, err := NewCurrentStaker(signedValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			err = baseState.UpdateCurrentDelegator(val)
			if !errors.Is(err, ErrUpdatingUnknownOrDeletedStaker) {
				return "unexpected update of delegator from unknown subnetID/nodeID"
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessDelegator, nil, nil, 1000),
	))

	var (
		subnetID = ids.GenerateTestID()
		nodeID   = ids.GenerateTestNodeID()
	)
	properties.Property("cannot update unknown delegator from known subnetID/nodeID", prop.ForAll(
		func(nonInitValTx, nonInitDelTx *txs.Tx) string {
			baseDBManager := manager.NewMemDB(version.Semantic1_0_0)
			baseDB := versiondb.New(baseDBManager.Current().Database)
			baseState, err := buildChainState(baseDB, nil)
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			signedValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedValTx.Unsigned.(txs.StakerTx)
			val, err := NewCurrentStaker(signedValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			signedDelTx, err := txs.NewSigned(nonInitDelTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx = signedDelTx.Unsigned.(txs.StakerTx)
			del, err := NewCurrentStaker(signedValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			baseState.PutCurrentValidator(val)
			err = baseState.UpdateCurrentDelegator(del)
			if !errors.Is(err, ErrUpdatingUnknownOrDeletedStaker) {
				return "unexpected update of unknown delegator from known subnetID/nodeID"
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessValidator, &subnetID, &nodeID, math.MaxUint64),
		stakerTxGenerator(ctx, permissionlessDelegator, &subnetID, &nodeID, 1000),
	))

	properties.TestingRun(t)
}

// TestDiffStakersProperties verifies properties specific to Diff, but not to State
func TestDiffStakersProperties(t *testing.T) {
	properties := gopter.NewProperties(nil)
	ctx := buildStateCtx()
	startTime := time.Now().Truncate(time.Second)

	properties.Property("updating unknown validator should not err", prop.ForAll(
		func(nonInitValTx *txs.Tx) string {
			diff, _, err := buildDiffOnTopOfBaseState(nil)
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			signedValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedValTx.Unsigned.(txs.StakerTx)
			val, err := NewCurrentStaker(signedValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			err = diff.UpdateCurrentValidator(val)
			if err != nil {
				return fmt.Sprintf("unexpected error while updating unknown validator in diff, %v", err)
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessValidator, nil, nil, math.MaxUint64),
	))

	properties.Property("updating deleted validator should err", prop.ForAll(
		func(nonInitValTx *txs.Tx) string {
			diff, _, err := buildDiffOnTopOfBaseState(nil)
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			signedValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedValTx.Unsigned.(txs.StakerTx)
			val, err := NewCurrentStaker(signedValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			diff.DeleteCurrentValidator(val) // mark validator as deleted

			err = diff.UpdateCurrentValidator(val)
			if !errors.Is(err, ErrUpdatingUnknownOrDeletedStaker) {
				return "expected error while updating validator in diff, got nil"
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessValidator, nil, nil, math.MaxUint64),
	))

	properties.Property("updating unknown delegator should not err", prop.ForAll(
		func(nonInitDelTx *txs.Tx) string {
			diff, _, err := buildDiffOnTopOfBaseState(nil)
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			signedDelTx, err := txs.NewSigned(nonInitDelTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedDelTx.Unsigned.(txs.StakerTx)
			del, err := NewCurrentStaker(signedDelTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			err = diff.UpdateCurrentDelegator(del)
			if err != nil {
				return fmt.Sprintf("unexpected error while updating unknown delegator in diff, %v", err)
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessDelegator, nil, nil, math.MaxUint64),
	))

	properties.Property("updating deleted delegator should err", prop.ForAll(
		func(nonInitDelTx *txs.Tx) string {
			diff, _, err := buildDiffOnTopOfBaseState(nil)
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			signedDelTx, err := txs.NewSigned(nonInitDelTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedDelTx.Unsigned.(txs.StakerTx)
			del, err := NewCurrentStaker(signedDelTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			diff.DeleteCurrentDelegator(del) // mark delegator as deleted

			err = diff.UpdateCurrentDelegator(del)
			if !errors.Is(err, ErrUpdatingUnknownOrDeletedStaker) {
				return "expected error while updating delegator in diff, got nil"
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessDelegator, nil, nil, math.MaxUint64),
	))

	properties.TestingRun(t)
}

// TestValidatorSetOperations verifies that validators set is duly updated
// upon different stakers operations
func TestValidatorSetOperations(t *testing.T) {
	properties := gopter.NewProperties(nil)
	ctx := buildStateCtx()
	startTime := time.Now().Truncate(time.Second)

	trackedSubnet := ids.GenerateTestID()
	properties.Property("validator is added upon staker insertion", prop.ForAll(
		func(nonInitValTx *txs.Tx) string {
			diff, baseState, err := buildDiffOnTopOfBaseState([]ids.ID{trackedSubnet})
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			signedValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedValTx.Unsigned.(txs.StakerTx)
			val, err := NewCurrentStaker(signedValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			diff.PutCurrentValidator(val)

			err = diff.Apply(baseState)
			if err != nil {
				return fmt.Sprintf("could not apply diff, err %v", err)
			}
			err = baseState.Commit()
			if err != nil {
				return fmt.Sprintf("could not commit state, err %v", err)
			}

			var (
				subnetID = val.SubnetID
				nodeID   = val.NodeID
			)

			set, found := baseState.(*state).cfg.Validators.Get(subnetID)
			if !found {
				return errMissingValidatorSet.Error()
			}

			if !set.Contains(nodeID) {
				return errMissingValidatotFromValidatorSet.Error()
			}

			if set.GetWeight(nodeID) != val.Weight {
				return "inserted staker's weight does not match with validator's weight in validator set"
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessValidator, &trackedSubnet, nil, math.MaxUint64),
	))

	properties.Property("validator is updated upon staker update", prop.ForAll(
		func(nonInitValTx *txs.Tx) string {
			diff, baseState, err := buildDiffOnTopOfBaseState([]ids.ID{trackedSubnet})
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			signedValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedValTx.Unsigned.(txs.StakerTx)
			val, err := NewCurrentStaker(signedValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			diff.PutCurrentValidator(val)

			updatedStaker := *val
			ShiftStakerAheadInPlace(&updatedStaker, updatedStaker.EndTime)
			IncreaseStakerWeightInPlace(&updatedStaker, updatedStaker.Weight*2)
			err = diff.UpdateCurrentValidator(&updatedStaker)
			if err != nil {
				return fmt.Sprintf("could not update current validator, err %v", err)
			}

			err = diff.Apply(baseState)
			if err != nil {
				return fmt.Sprintf("could not apply diff, err %v", err)
			}
			err = baseState.Commit()
			if err != nil {
				return fmt.Sprintf("could not commit state, err %v", err)
			}

			var (
				subnetID = val.SubnetID
				nodeID   = val.NodeID
			)

			set, found := baseState.(*state).cfg.Validators.Get(subnetID)
			if !found {
				return errMissingValidatorSet.Error()
			}

			if !set.Contains(nodeID) {
				return errMissingValidatotFromValidatorSet.Error()
			}

			if set.GetWeight(nodeID) != updatedStaker.Weight {
				return "inserted staker's weight does not match with validator's weight in validator set"
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessValidator, &trackedSubnet, nil, math.MaxUint64),
	))

	properties.Property("validator is deleted upon staker delete", prop.ForAll(
		func(nonInitMainValTx, nonInitCompanionValTx *txs.Tx) string {
			diff, baseState, err := buildDiffOnTopOfBaseState([]ids.ID{trackedSubnet})
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			signedMainValTx, err := txs.NewSigned(nonInitMainValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedMainValTx.Unsigned.(txs.StakerTx)
			mainVal, err := NewCurrentStaker(signedMainValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			signedCompanionValTx, err := txs.NewSigned(nonInitCompanionValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx = signedCompanionValTx.Unsigned.(txs.StakerTx)
			companionVal, err := NewCurrentStaker(signedCompanionValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			diff.PutCurrentValidator(mainVal)
			diff.PutCurrentValidator(companionVal)
			diff.DeleteCurrentValidator(mainVal)

			err = diff.Apply(baseState)
			if err != nil {
				return fmt.Sprintf("could not apply diff, err %v", err)
			}
			err = baseState.Commit()
			if err != nil {
				return fmt.Sprintf("could not commit state, err %v", err)
			}

			var (
				subnetID = mainVal.SubnetID
				nodeID   = mainVal.NodeID
			)

			set, found := baseState.(*state).cfg.Validators.Get(subnetID)
			if !found {
				return errMissingValidatorSet.Error()
			}

			if set.Contains(nodeID) {
				return errUnexpectedValidatotInValidatorSet.Error()
			}

			if set.GetWeight(nodeID) != 0 {
				return "deleted validators's weight is not zero"
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessValidator, &trackedSubnet, nil, math.MaxUint64),
		stakerTxGenerator(ctx, permissionlessDelegator, &trackedSubnet, nil, math.MaxUint64),
	))

	properties.TestingRun(t)
}

// TestValidatorSetOperations verifies that validators set is duly updated
// upon different stakers operations
func TestValidatorUptimesOperations(t *testing.T) {
	properties := gopter.NewProperties(nil)
	ctx := buildStateCtx()
	startTime := time.Now().Truncate(time.Second)

	// // to reproduce a given scenario do something like this:
	// parameters := gopter.DefaultTestParametersWithSeed(1688585045068172845)
	// properties := gopter.NewProperties(parameters)

	properties.Property("staker start time is updated following shift", prop.ForAll(
		func(nonInitValTx *txs.Tx) string {
			diff, baseState, err := buildDiffOnTopOfBaseState([]ids.ID{})
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			signedValTx, err := txs.NewSigned(nonInitValTx.Unsigned, txs.Codec, nil)
			if err != nil {
				panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
			}

			stakerTx := signedValTx.Unsigned.(txs.StakerTx)
			val, err := NewCurrentStaker(signedValTx.ID(), stakerTx, startTime, uint64(1000))
			if err != nil {
				return err.Error()
			}

			// 1. Insert the validator
			var (
				subnetID = val.SubnetID
				nodeID   = val.NodeID
				duration = 24 * time.Hour
			)
			diff.PutCurrentValidator(val)
			err = diff.Apply(baseState)
			if err != nil {
				return fmt.Sprintf("could not apply diff, err %v", err)
			}

			err = baseState.Commit()
			if err != nil {
				return fmt.Sprintf("could not commit state, err %v", err)
			}

			err = baseState.SetUptime(nodeID, subnetID, duration, val.StartTime)
			if err != nil {
				return fmt.Sprintf("could not set uptime, err %v", err)
			}

			// Check start time
			startTime, err := baseState.GetStartTime(nodeID, subnetID)
			if err != nil {
				return fmt.Sprintf("could not get validator start time, err %v", err)
			}
			if !startTime.Equal(val.StartTime) {
				return fmt.Sprintf("wrong start time, expected %v, got %v", val.StartTime, startTime)
			}

			// Check uptimes
			uptime, lastUpdated, err := baseState.GetUptime(nodeID, subnetID)
			if err != nil {
				return fmt.Sprintf("could not get validator uptime, err %v", err)
			}
			if !lastUpdated.Equal(val.StartTime) {
				return fmt.Sprintf("wrong start time, expected %v, got %v", val.StartTime, startTime)
			}
			if uptime != duration {
				return fmt.Sprintf("wrong uptime, expected %v, got %v", uptime, duration)
			}

			// 2. Shift the validator
			updatedStaker := *val
			ShiftStakerAheadInPlace(&updatedStaker, updatedStaker.EndTime)
			IncreaseStakerWeightInPlace(&updatedStaker, updatedStaker.Weight/2)

			diff, err = NewDiff(baseState.GetLastAccepted(), &versionsHolder{
				baseState: baseState,
			})
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			err = diff.UpdateCurrentValidator(&updatedStaker)
			if err != nil {
				return fmt.Sprintf("could not update current validator, err %v", err)
			}

			err = diff.Apply(baseState)
			if err != nil {
				return fmt.Sprintf("could not apply diff, err %v", err)
			}
			err = baseState.Commit()
			if err != nil {
				return fmt.Sprintf("could not commit state, err %v", err)
			}

			// Check start time
			startTime, err = baseState.GetStartTime(nodeID, subnetID)
			if err != nil {
				return fmt.Sprintf("could not get validator start time, err %v", err)
			}
			if !startTime.Equal(updatedStaker.StartTime) {
				return fmt.Sprintf("wrong updated start time, expected %v, got %v", updatedStaker.StartTime, startTime)
			}

			// Check uptimes
			uptime, lastUpdated, err = baseState.GetUptime(nodeID, subnetID)
			if err != nil {
				return fmt.Sprintf("could not get validator uptime, err %v", err)
			}
			if !lastUpdated.Equal(updatedStaker.StartTime) {
				return fmt.Sprintf("wrong updated start time, expected %v, got %v", val.StartTime, startTime)
			}
			if uptime != 0 {
				return fmt.Sprintf("wrong uptime, expected %v, got %v", uptime, 0)
			}

			return ""
		},
		stakerTxGenerator(ctx, permissionlessValidator, &constants.PrimaryNetworkID, nil, math.MaxUint64),
	))

	properties.TestingRun(t)
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

// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package models

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

var errNonEmptyIteratorExpected = errors.New("expected non-empty iterator, got no elements")

func TestSimpleStakersOperations(t *testing.T) {
	storeCreators := map[string]func() (state.Stakers, error){
		"base state": func() (state.Stakers, error) {
			return buildChainState()
		},
		"diff": func() (state.Stakers, error) {
			baseState, err := buildChainState()
			if err != nil {
				return nil, fmt.Errorf("unexpected error while creating chain base state, err %v", err)
			}

			genesisID := baseState.GetLastAccepted()
			versions := &versionsHolder{
				baseState: baseState,
			}
			store, err := state.NewDiff(genesisID, versions)
			if err != nil {
				return nil, fmt.Errorf("unexpected error while creating diff, err %v", err)
			}
			return store, nil
		},
		"storage model": func() (state.Stakers, error) { //nolint:golint,unparam
			return newStakersStorageModel(), nil
		},
	}

	for storeType, storeCreatorF := range storeCreators {
		t.Run(storeType, func(t *testing.T) {
			properties := simpleStakerStateProperties(storeCreatorF)
			properties.TestingRun(t)
		})
	}
}

func simpleStakerStateProperties(storeCreatorF func() (state.Stakers, error)) *gopter.Properties {
	properties := gopter.NewProperties(nil)

	properties.Property("add, delete and query current validators", prop.ForAll(
		func(s state.Staker) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			// no staker before insertion
			_, err = store.GetCurrentValidator(s.SubnetID, s.NodeID)
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}

			// it's fine deleting unknown validator
			store.DeleteCurrentValidator(&s)

			currIT, err := store.GetCurrentStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if currIT.Next() {
				return fmt.Sprintf("expected empty iterator, got at least element %v", currIT.Value())
			}
			currIT.Release()

			// staker after insertion
			store.PutCurrentValidator(&s)
			retrievedStaker, err := store.GetCurrentValidator(s.SubnetID, s.NodeID)
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(&s, retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}

			currIT, err = store.GetCurrentStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if !currIT.Next() {
				return errNonEmptyIteratorExpected.Error()
			}
			if !reflect.DeepEqual(currIT.Value(), retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}
			currIT.Release()

			// no staker after deletion
			store.DeleteCurrentValidator(&s)
			_, err = store.GetCurrentValidator(s.SubnetID, s.NodeID)
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}

			currIT, err = store.GetCurrentStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if currIT.Next() {
				return fmt.Sprintf("expected empty iterator, got at least element %v", currIT.Value())
			}
			currIT.Release()

			return ""
		},
		stakerGenerator(anyPriority, nil, nil),
	))

	properties.Property("update current validators", prop.ForAll(
		func(s state.Staker) string {
			// insert staker s first, then update StartTime/EndTime and update the staker
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			store.PutCurrentValidator(&s)
			retrievedStaker, err := store.GetCurrentValidator(s.SubnetID, s.NodeID)
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(&s, retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}

			currIT, err := store.GetCurrentStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if !currIT.Next() {
				return errNonEmptyIteratorExpected.Error()
			}
			if !reflect.DeepEqual(currIT.Value(), retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}
			currIT.Release()

			// update staker times as expected. We copy the updated staker
			// to avoid in-place modification of stakers already stored in store,
			// as it must be done in prod code.
			updatedStaker := s
			state.RotateStakerTimesInPlace(&updatedStaker)

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
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}

			currIT, err = store.GetCurrentStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if !currIT.Next() {
				return errNonEmptyIteratorExpected.Error()
			}
			if !reflect.DeepEqual(currIT.Value(), retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}
			currIT.Release()
			return ""
		},
		stakerGenerator(currentValidator, nil, nil),
	))

	properties.Property("add, delete and query pending validators", prop.ForAll(
		func(s state.Staker) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			// no staker before insertion
			_, err = store.GetPendingValidator(s.SubnetID, s.NodeID)
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}

			// it's fine deleting unknown validator
			store.DeletePendingValidator(&s)

			currIT, err := store.GetPendingStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if currIT.Next() {
				return fmt.Sprintf("expected empty iterator, got at least element %v", currIT.Value())
			}
			currIT.Release()

			// staker after insertion
			store.PutPendingValidator(&s)
			retrievedStaker, err := store.GetPendingValidator(s.SubnetID, s.NodeID)
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(&s, retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}

			currIT, err = store.GetPendingStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if !currIT.Next() {
				return errNonEmptyIteratorExpected.Error()
			}
			if !reflect.DeepEqual(currIT.Value(), retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}
			currIT.Release()

			// no staker after deletion
			store.DeletePendingValidator(&s)
			_, err = store.GetPendingValidator(s.SubnetID, s.NodeID)
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}

			currIT, err = store.GetPendingStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if currIT.Next() {
				return fmt.Sprintf("expected empty iterator, got at least element %v", currIT.Value())
			}
			currIT.Release()

			return ""
		},
		stakerGenerator(anyPriority, nil, nil),
	))

	var (
		subnetID = ids.GenerateTestID()
		nodeID   = ids.GenerateTestNodeID()
	)
	properties.Property("add, delete and query current delegators", prop.ForAll(
		func(val state.Staker, dels []state.Staker) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			// store validator
			store.PutCurrentValidator(&val)

			// check validator - version 1
			retrievedValidator, err := store.GetCurrentValidator(val.SubnetID, val.NodeID)
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(&val, retrievedValidator) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &val, retrievedValidator)
			}

			// check validator - version 2
			valIt, err := store.GetCurrentStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if !valIt.Next() {
				return errNonEmptyIteratorExpected.Error()
			}
			if !reflect.DeepEqual(valIt.Value(), retrievedValidator) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &val, retrievedValidator)
			}
			valIt.Release()

			// store delegators
			for _, del := range dels {
				cpy := del

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
					if reflect.DeepEqual(*delIt.Value(), del) {
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
					if reflect.DeepEqual(*delIt.Value(), del) {
						found = true
						break
					}
				}
				if !found {
					return fmt.Sprintf("found extra delegator %v", delIt.Value())
				}
			}
			delIt.Release()

			// check no missing delegators if whole staker set
			for _, del := range dels {
				found := false
				fullDelIt, err := store.GetCurrentStakerIterator()
				if err != nil {
					return fmt.Sprintf("unexpected failure in current delegators iterator creation, error %v", err)
				}
				for fullDelIt.Next() {
					if reflect.DeepEqual(*fullDelIt.Value(), del) {
						found = true
						break
					}
				}
				fullDelIt.Release()

				if !found {
					return fmt.Sprintf("missing delegator %v", del)
				}
			}

			// delete delegators
			for _, del := range dels {
				cpy := del
				store.DeleteCurrentDelegator(&cpy)

				// check deleted delegator is not there anymore
				delIt, err := store.GetCurrentDelegatorIterator(subnetID, nodeID)
				if err != nil {
					return fmt.Sprintf("unexpected failure in current delegators iterator creation, error %v", err)
				}

				found := false
				for delIt.Next() {
					if reflect.DeepEqual(*delIt.Value(), del) {
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
		stakerGenerator(currentValidator, &subnetID, &nodeID),

		// TODO ABENEGIA: make sure txIDs are unique in slice.
		// They are unlikely to be equal, but still should be fixed.
		gen.SliceOfN(10, stakerGenerator(currentDelegator, &subnetID, &nodeID)).
			SuchThat(func(v interface{}) bool {
				stakersList := v.([]state.Staker)
				uniqueTxIDs := set.NewSet[ids.ID](len(stakersList))
				for _, staker := range stakersList {
					uniqueTxIDs.Add(staker.TxID)
				}

				// make sure TxIDs are unique, at least among delegators
				return len(stakersList) == uniqueTxIDs.Len()
			}),
	))

	properties.Property("add, delete and query pending delegators", prop.ForAll(
		func(val state.Staker, dels []state.Staker) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			// store validator
			store.PutCurrentValidator(&val)

			// check validator - version 1
			retrievedValidator, err := store.GetCurrentValidator(val.SubnetID, val.NodeID)
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(&val, retrievedValidator) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &val, retrievedValidator)
			}

			// check validator - version 2
			valIt, err := store.GetCurrentStakerIterator()
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if !valIt.Next() {
				return errNonEmptyIteratorExpected.Error()
			}
			if !reflect.DeepEqual(valIt.Value(), retrievedValidator) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &val, retrievedValidator)
			}
			valIt.Release()

			// store delegators
			for _, del := range dels {
				cpy := del

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
					if reflect.DeepEqual(*delIt.Value(), del) {
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
					if reflect.DeepEqual(*delIt.Value(), del) {
						found = true
						break
					}
				}
				if !found {
					return fmt.Sprintf("found extra delegator %v", delIt.Value())
				}
			}
			delIt.Release()

			// check no missing delegators if whole staker set
			for _, del := range dels {
				found := false
				fullDelIt, err := store.GetPendingStakerIterator()
				if err != nil {
					return fmt.Sprintf("unexpected failure in current delegators iterator creation, error %v", err)
				}
				for fullDelIt.Next() {
					if reflect.DeepEqual(*fullDelIt.Value(), del) {
						found = true
						break
					}
				}
				fullDelIt.Release()

				if !found {
					return fmt.Sprintf("missing delegator %v", del)
				}
			}

			// delete delegators
			for _, del := range dels {
				cpy := del
				store.DeletePendingDelegator(&cpy)

				// check deleted delegator is not there anymore
				delIt, err := store.GetPendingDelegatorIterator(subnetID, nodeID)
				if err != nil {
					return fmt.Sprintf("unexpected failure in pending delegators iterator creation, error %v", err)
				}

				found := false
				for delIt.Next() {
					if reflect.DeepEqual(*delIt.Value(), del) {
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
		stakerGenerator(currentValidator, &subnetID, &nodeID),
		gen.SliceOfN(10, stakerGenerator(pendingDelegator, &subnetID, &nodeID)).
			SuchThat(func(v interface{}) bool {
				stakersList := v.([]state.Staker)
				uniqueTxIDs := set.NewSet[ids.ID](len(stakersList))
				for _, staker := range stakersList {
					uniqueTxIDs.Add(staker.TxID)
				}

				// make sure TxIDs are unique, at least among delegators
				return len(stakersList) == uniqueTxIDs.Len()
			}),
	))

	return properties
}

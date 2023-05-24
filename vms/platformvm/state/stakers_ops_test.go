// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestSimpleStakersOperations checks that State and Diff conform our stakersStorageModel.
// TestSimpleStakersOperations tests State and Diff in isolation, over simple operations.
// TestStateAndDiffComparisonToStorageModel carries a more involved verification over a production-like
// mix of State and Diffs.
func TestSimpleStakersOperations(t *testing.T) {
	storeCreators := map[string]func() (Stakers, error){
		"base state": func() (Stakers, error) {
			return buildChainState()
		},
		"diff": func() (Stakers, error) {
			baseState, err := buildChainState()
			if err != nil {
				return nil, fmt.Errorf("unexpected error while creating chain base state, err %v", err)
			}

			genesisID := baseState.GetLastAccepted()
			versions := &versionsHolder{
				baseState: baseState,
			}
			store, err := NewDiff(genesisID, versions)
			if err != nil {
				return nil, fmt.Errorf("unexpected error while creating diff, err %v", err)
			}
			return store, nil
		},
		"storage model": func() (Stakers, error) { //nolint:golint,unparam
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

func simpleStakerStateProperties(storeCreatorF func() (Stakers, error)) *gopter.Properties {
	properties := gopter.NewProperties(nil)

	properties.Property("add, delete and query current validators", prop.ForAll(
		func(s Staker) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			// no staker before insertion
			_, err = store.GetCurrentValidator(s.SubnetID, s.NodeID) // check version 1
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}
			err = checkCurrentStakersContent(store, []Staker{})
			if err != nil {
				return err.Error()
			}

			// it's fine deleting unknown validator
			store.DeleteCurrentValidator(&s)
			_, err = store.GetCurrentValidator(s.SubnetID, s.NodeID) // check version 1
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}
			err = checkCurrentStakersContent(store, []Staker{})
			if err != nil {
				return err.Error()
			}

			// insert the staker and show it can be found
			store.PutCurrentValidator(&s)
			retrievedStaker, err := store.GetCurrentValidator(s.SubnetID, s.NodeID) // check version 1
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(&s, retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}
			err = checkCurrentStakersContent(store, []Staker{s})
			if err != nil {
				return err.Error()
			}

			// delete the staker and show it won't be found anymore
			store.DeleteCurrentValidator(&s)
			_, err = store.GetCurrentValidator(s.SubnetID, s.NodeID) // check version 1
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}
			err = checkCurrentStakersContent(store, []Staker{})
			if err != nil {
				return err.Error()
			}

			return ""
		},
		stakerGenerator(anyPriority, nil, nil, math.MaxUint64),
	))

	properties.Property("add, delete and query pending validators", prop.ForAll(
		func(s Staker) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			// no staker before insertion
			_, err = store.GetPendingValidator(s.SubnetID, s.NodeID) // check version 1
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}

			pendIt, err := store.GetPendingStakerIterator() // check version 2
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if pendIt.Next() {
				return fmt.Sprintf("expected empty iterator, got at least element %v", pendIt.Value())
			}
			pendIt.Release()

			// it's fine deleting unknown validator
			store.DeletePendingValidator(&s)
			_, err = store.GetPendingValidator(s.SubnetID, s.NodeID) // check version 1
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}

			pendIt, err = store.GetPendingStakerIterator() // check version 2
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if pendIt.Next() {
				return fmt.Sprintf("expected empty iterator, got at least element %v", pendIt.Value())
			}
			pendIt.Release()

			// insert the staker and show it can be found
			store.PutPendingValidator(&s)
			retrievedStaker, err := store.GetPendingValidator(s.SubnetID, s.NodeID) // check version 1
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(&s, retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}

			pendIt, err = store.GetPendingStakerIterator() // check version 2
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if !pendIt.Next() {
				return errNonEmptyIteratorExpected.Error()
			}
			if !reflect.DeepEqual(pendIt.Value(), retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}
			pendIt.Release()

			// delete the staker and show it won't be found anymore
			store.DeletePendingValidator(&s)
			_, err = store.GetPendingValidator(s.SubnetID, s.NodeID) // check version 1
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}

			pendIt, err = store.GetPendingStakerIterator() // check version 2
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if pendIt.Next() {
				return fmt.Sprintf("expected empty iterator, got at least element %v", pendIt.Value())
			}
			pendIt.Release()

			return ""
		},
		stakerGenerator(anyPriority, nil, nil, math.MaxUint64),
	))

	var (
		subnetID = ids.GenerateTestID()
		nodeID   = ids.GenerateTestNodeID()
	)
	properties.Property("add, delete and query current delegators", prop.ForAll(
		func(val Staker, dels []Staker) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			// store validator
			store.PutCurrentValidator(&val)
			retrievedValidator, err := store.GetCurrentValidator(val.SubnetID, val.NodeID) // check version 1
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(&val, retrievedValidator) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &val, retrievedValidator)
			}
			err = checkCurrentStakersContent(store, []Staker{val})
			if err != nil {
				return err.Error()
			}

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

			// check no missing delegators in the whole staker set
			stakersSet := dels
			stakersSet = append(stakersSet, val)
			err = checkCurrentStakersContent(store, stakersSet)
			if err != nil {
				return err.Error()
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
		stakerGenerator(currentValidator, &subnetID, &nodeID, math.MaxUint64),
		gen.SliceOfN(10, stakerGenerator(currentDelegator, &subnetID, &nodeID, math.MaxUint64)).
			SuchThat(func(v interface{}) bool {
				stakersList := v.([]Staker)
				uniqueTxIDs := set.NewSet[ids.ID](len(stakersList))
				for _, staker := range stakersList {
					uniqueTxIDs.Add(staker.TxID)
				}

				// make sure TxIDs are unique, at least among delegators.
				return len(stakersList) == uniqueTxIDs.Len()
			}),
	))

	properties.Property("add, delete and query pending delegators", prop.ForAll(
		func(val Staker, dels []Staker) string {
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			// store validator
			store.PutCurrentValidator(&val)
			retrievedValidator, err := store.GetCurrentValidator(val.SubnetID, val.NodeID) // check version 1
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(&val, retrievedValidator) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &val, retrievedValidator)
			}

			err = checkCurrentStakersContent(store, []Staker{val})
			if err != nil {
				return err.Error()
			}

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

			// check no missing delegators in the whole staker set
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
		stakerGenerator(currentValidator, &subnetID, &nodeID, math.MaxUint64),
		gen.SliceOfN(10, stakerGenerator(pendingDelegator, &subnetID, &nodeID, math.MaxUint64)).
			SuchThat(func(v interface{}) bool {
				stakersList := v.([]Staker)
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

// verify wheter store contains exactly the stakers specify in the list.
// stakers order does not matter. Also stakers get consumes while checking
func checkCurrentStakersContent(store Stakers, stakers []Staker) error {
	currIT, err := store.GetCurrentStakerIterator()
	if err != nil {
		return fmt.Errorf("unexpected failure in staker iterator creation, error %v", err)
	}
	defer currIT.Release()

	if len(stakers) == 0 {
		if currIT.Next() {
			return fmt.Errorf("expected empty iterator, got at least element %v", currIT.Value())
		}
		return nil
	}

	for currIT.Next() {
		var (
			staker = currIT.Value()
			found  = false

			retrievedStakerIdx = 0
		)

		for idx, s := range stakers {
			if reflect.DeepEqual(staker, &s) {
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
		return fmt.Errorf("missing stakers")
	}
	return nil
}

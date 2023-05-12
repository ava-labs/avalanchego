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

// TestGeneralStakerContainersProperties checks that State and Diff conform our stakersStorageModel.
// TestGeneralStakerContainersProperties tests State and Diff in isolation, over simple operations.
// TestStateAndDiffComparisonToStorageModel carries a more involved verification over a production-like
// mix of State and Diffs.
func TestGeneralStakerContainersProperties(t *testing.T) {
	storeCreators := map[string]func() (Stakers, error){
		"base state": func() (Stakers, error) {
			return buildChainState()
		},
		"diff": func() (Stakers, error) {
			diff, err := buildDiffOnTopOfBaseState()
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

			currIT, err := store.GetCurrentStakerIterator() // check version 2
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if currIT.Next() {
				return fmt.Sprintf("expected empty iterator, got at least element %v", currIT.Value())
			}
			currIT.Release()

			// it's fine deleting unknown validator
			store.DeleteCurrentValidator(&s)
			_, err = store.GetCurrentValidator(s.SubnetID, s.NodeID) // check version 1
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}

			currIT, err = store.GetCurrentStakerIterator() // check version 2
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if currIT.Next() {
				return fmt.Sprintf("expected empty iterator, got at least element %v", currIT.Value())
			}
			currIT.Release()

			// insert the staker and show it can be found
			store.PutCurrentValidator(&s)
			retrievedStaker, err := store.GetCurrentValidator(s.SubnetID, s.NodeID) // check version 1
			if err != nil {
				return fmt.Sprintf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(&s, retrievedStaker) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &s, retrievedStaker)
			}

			currIT, err = store.GetCurrentStakerIterator() // check version 2
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

			// delete the staker and show it won't be found anymore
			store.DeleteCurrentValidator(&s)
			_, err = store.GetCurrentValidator(s.SubnetID, s.NodeID) // check version 1
			if err != database.ErrNotFound {
				return fmt.Sprintf("unexpected error %v, got %v", database.ErrNotFound, err)
			}

			currIT, err = store.GetCurrentStakerIterator() // check version 2
			if err != nil {
				return fmt.Sprintf("unexpected failure in staker iterator creation, error %v", err)
			}
			if currIT.Next() {
				return fmt.Sprintf("expected empty iterator, got at least element %v", currIT.Value())
			}
			currIT.Release()

			return ""
		},
		stakerGenerator(anyPriority, nil, nil, math.MaxUint64),
	))

	properties.Property("update current validators", prop.ForAll(
		func(s Staker) string {
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
			RotateStakerTimesInPlace(&updatedStaker)

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
		stakerGenerator(currentValidator, nil, nil, math.MaxUint64),
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

			valIt, err := store.GetCurrentStakerIterator() // check version 2
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

			// check no missing delegators in the whole staker set
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

	properties.Property("update current delegator", prop.ForAll(
		func(dels []Staker) string {
			// insert staker s first, then update StartTime/EndTime and update the staker
			store, err := storeCreatorF()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			// store delegators
			for _, del := range dels {
				cpy := del
				store.PutCurrentDelegator(&cpy)
			}

			// update delegators
			for _, del := range dels {
				// update staker times as expected. We copy the updated staker
				// to avoid in-place modification of stakers already stored in store,
				// as it must be done in prod code.
				updatedStaker := del
				RotateStakerTimesInPlace(&updatedStaker)

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

			valIt, err := store.GetCurrentStakerIterator() // check version 2
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

// TestStateStakersProperties verifies properties specific to State, but not to Diff
func TestStateStakersProperties(t *testing.T) {
	properties := gopter.NewProperties(nil)
	properties.Property("cannot update unknown validator", prop.ForAll(
		func(s Staker) string {
			baseState, err := buildChainState()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			err = baseState.UpdateCurrentValidator(&s)
			if err == nil {
				return "unexpected update of unknown validator"
			}

			return ""
		},
		stakerGenerator(currentValidator, nil, nil, math.MaxUint64),
	))

	properties.Property("cannot update deleted validator", prop.ForAll(
		func(s Staker) string {
			baseState, err := buildChainState()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			baseState.PutCurrentValidator(&s)
			baseState.DeleteCurrentValidator(&s)
			err = baseState.UpdateCurrentValidator(&s)
			if err == nil {
				return "unexpected update of unknown validator"
			}

			return ""
		},
		stakerGenerator(currentValidator, nil, nil, math.MaxUint64),
	))

	properties.Property("cannot update delegator from unknown subnetID/nodeID", prop.ForAll(
		func(s Staker) string {
			baseState, err := buildChainState()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			err = baseState.UpdateCurrentDelegator(&s)
			if err == nil {
				return "unexpected update of delegator from unknown subnetID/nodeID"
			}

			return ""
		},
		stakerGenerator(currentDelegator, nil, nil, math.MaxUint64),
	))

	var (
		subnetID = ids.GenerateTestID()
		nodeID   = ids.GenerateTestNodeID()
	)
	properties.Property("cannot update unknown delegator from known subnetID/nodeID", prop.ForAll(
		func(val, del Staker) string {
			baseState, err := buildChainState()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating staker store, err %v", err)
			}

			baseState.PutCurrentValidator(&val)
			err = baseState.UpdateCurrentDelegator(&del)
			if err == nil {
				return "unexpected update of unknown delegator from known subnetID/nodeID"
			}

			return ""
		},
		stakerGenerator(currentValidator, &subnetID, &nodeID, math.MaxUint64),
		stakerGenerator(currentDelegator, &subnetID, &nodeID, math.MaxUint64),
	))

	properties.TestingRun(t)
}

// TestDiffStakersProperties verifies properties specific to Diff, but not to State
func TestDiffStakersProperties(t *testing.T) {
	properties := gopter.NewProperties(nil)
	properties.Property("updating unknown validator won't err", prop.ForAll(
		func(s Staker) string {
			diff, err := buildDiffOnTopOfBaseState()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			err = diff.UpdateCurrentValidator(&s)
			if err != nil {
				return fmt.Sprintf("unexpected error while updating unknown validator in diff, %v", err)
			}

			return ""
		},
		stakerGenerator(currentValidator, nil, nil, math.MaxUint64),
	))

	properties.Property("updating deleted validator does err", prop.ForAll(
		func(s Staker) string {
			diff, err := buildDiffOnTopOfBaseState()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			diff.DeleteCurrentValidator(&s) // mark validator as deleted

			err = diff.UpdateCurrentValidator(&s)
			if err == nil {
				return "expected error while updating validator in diff, got nil"
			}

			return ""
		},
		stakerGenerator(currentValidator, nil, nil, math.MaxUint64),
	))

	properties.Property("updating unknown delegator won't err", prop.ForAll(
		func(s Staker) string {
			diff, err := buildDiffOnTopOfBaseState()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			err = diff.UpdateCurrentDelegator(&s)
			if err != nil {
				return fmt.Sprintf("unexpected error while updating unknown delegator in diff, %v", err)
			}

			return ""
		},
		stakerGenerator(currentDelegator, nil, nil, math.MaxUint64),
	))

	properties.Property("updating deleted delegator does err", prop.ForAll(
		func(s Staker) string {
			diff, err := buildDiffOnTopOfBaseState()
			if err != nil {
				return fmt.Sprintf("unexpected error while creating diff, err %v", err)
			}

			diff.DeleteCurrentDelegator(&s) // mark delegator as deleted

			err = diff.UpdateCurrentDelegator(&s)
			if err == nil {
				return "expected error while updating delegator in diff, got nil"
			}

			return ""
		},
		stakerGenerator(currentDelegator, nil, nil, math.MaxUint64),
	))

	properties.TestingRun(t)
}

func buildDiffOnTopOfBaseState() (Diff, error) {
	baseState, err := buildChainState()
	if err != nil {
		return nil, fmt.Errorf("unexpected error while creating chain base state, err %v", err)
	}

	genesisID := baseState.GetLastAccepted()
	versions := &versionsHolder{
		baseState: baseState,
	}
	diff, err := NewDiff(genesisID, versions)
	if err != nil {
		return nil, fmt.Errorf("unexpected error while creating diff, err %v", err)
	}
	return diff, nil
}

// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package models

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

func TestStakersStorageMode(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("some current validator ops", prop.ForAll(
		func(s state.Staker) string {
			store := newStakersStorageModel()

			// no staker before insertion
			_, err := store.GetCurrentValidator(s.SubnetID, s.NodeID)
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
				return "expected non-empty iterator, got no elements"
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
		stakerGenerator(nil, nil, nil),
	))

	properties.Property("some pending validator ops", prop.ForAll(
		func(s state.Staker) string {
			store := newStakersStorageModel()

			// no staker before insertion
			_, err := store.GetPendingValidator(s.SubnetID, s.NodeID)
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
				return "expected non-empty iterator, got no elements"
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
		stakerGenerator(nil, nil, nil),
	))

	var (
		valPrio  = currentValidator
		delPrio  = currentDelegator
		subnetID = ids.GenerateTestID()
		nodeID   = ids.GenerateTestNodeID()
	)
	properties.Property("some current delegators ops", prop.ForAll(
		func(val state.Staker, dels []state.Staker) string {
			store := newStakersStorageModel()

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
				return "expected non-empty iterator, got no elements"
			}
			if !reflect.DeepEqual(valIt.Value(), retrievedValidator) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &val, retrievedValidator)
			}
			valIt.Release()

			// store delegators
			for _, del := range dels {
				cpy := del
				store.PutCurrentDelegator(&cpy)
			}

			// check delegators - version 1
			delIt, err := store.GetCurrentDelegatorIterator(subnetID, nodeID)
			if err != nil {
				return fmt.Sprintf("unexpected failure in current delegators iterator creation, error %v", err)
			}
			for delIt.Next() {
				found := false
				for _, del := range dels {
					if reflect.DeepEqual(delIt.Value(), &del) {
						found = true
						break
					}
				}
				if !found {
					return fmt.Sprintf("found extra delegator %v", delIt.Value())
				}
			}
			delIt.Release()

			// check delegators - version 2
			for _, del := range dels {
				found := false
				delIt, err := store.GetCurrentDelegatorIterator(subnetID, nodeID)
				if err != nil {
					return fmt.Sprintf("unexpected failure in current delegators iterator creation, error %v", err)
				}
				for delIt.Next() {
					if reflect.DeepEqual(delIt.Value(), &del) {
						found = true
						break
					}
				}
				delIt.Release()

				if !found {
					return fmt.Sprintf("missing delegator %v", del)
				}
			}

			// delege delegators
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
					if reflect.DeepEqual(delIt.Value(), &del) {
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
		stakerGenerator(&valPrio, &subnetID, &nodeID),
		gen.SliceOfN(20, stakerGenerator(&delPrio, &subnetID, &nodeID)),
	))

	properties.Property("some pending delegators ops", prop.ForAll(
		func(val state.Staker, dels []state.Staker) string {
			store := newStakersStorageModel()

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
				return "expected non-empty iterator, got no elements"
			}
			if !reflect.DeepEqual(valIt.Value(), retrievedValidator) {
				return fmt.Sprintf("wrong staker retrieved expected %v, got %v", &val, retrievedValidator)
			}
			valIt.Release()

			// store delegators
			for _, del := range dels {
				cpy := del
				store.PutPendingDelegator(&cpy)
			}

			// check delegators - version 1
			delIt, err := store.GetPendingDelegatorIterator(subnetID, nodeID)
			if err != nil {
				return fmt.Sprintf("unexpected failure in pending delegators iterator creation, error %v", err)
			}
			for delIt.Next() {
				found := false
				for _, del := range dels {
					if reflect.DeepEqual(delIt.Value(), &del) {
						found = true
						break
					}
				}
				if !found {
					return fmt.Sprintf("found extra delegator %v", delIt.Value())
				}
			}
			delIt.Release()

			// check delegators - version 2
			for _, del := range dels {
				found := false
				delIt, err := store.GetPendingDelegatorIterator(subnetID, nodeID)
				if err != nil {
					return fmt.Sprintf("unexpected failure in pending delegators iterator creation, error %v", err)
				}
				for delIt.Next() {
					if reflect.DeepEqual(delIt.Value(), &del) {
						found = true
						break
					}
				}
				delIt.Release()

				if !found {
					return fmt.Sprintf("missing delegator %v", del)
				}
			}

			// delege delegators
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
					if reflect.DeepEqual(delIt.Value(), &del) {
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
		stakerGenerator(&valPrio, &subnetID, &nodeID),
		gen.SliceOfN(20, stakerGenerator(&delPrio, &subnetID, &nodeID)),
	))

	properties.TestingRun(t)
}

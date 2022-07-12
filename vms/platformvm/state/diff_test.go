// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestDiffMissingState(t *testing.T) {
	assert := assert.New(t)

	state, _ := newInitializedState(assert)
	states := NewVersions(ids.GenerateTestID(), state)

	_, err := NewDiff(ids.GenerateTestID(), states)
	assert.ErrorIs(err, errMissingParentState)
}

func TestDiffCreation(t *testing.T) {
	assert := assert.New(t)

	lastAcceptedID := ids.GenerateTestID()
	state, _ := newInitializedState(assert)
	states := NewVersions(lastAcceptedID, state)

	d, err := NewDiff(lastAcceptedID, states)
	assert.NoError(err)
	assertChainsEqual(t, state, d)
}

func TestDiffCurrentSupply(t *testing.T) {
	assert := assert.New(t)

	lastAcceptedID := ids.GenerateTestID()
	state, _ := newInitializedState(assert)
	states := NewVersions(lastAcceptedID, state)

	d, err := NewDiff(lastAcceptedID, states)
	assert.NoError(err)

	initialCurrentSupply := d.GetCurrentSupply()
	newCurrentSupply := initialCurrentSupply + 1
	d.SetCurrentSupply(newCurrentSupply)
	assert.Equal(newCurrentSupply, d.GetCurrentSupply())
	assert.Equal(initialCurrentSupply, state.GetCurrentSupply())
}

func assertChainsEqual(t *testing.T, expected, actual Chain) {
	t.Helper()

	expectedCurrentStakerIterator, expectedErr := expected.GetCurrentStakerIterator()
	actualCurrentStakerIterator, actualErr := actual.GetCurrentStakerIterator()
	assert.Equal(t, expectedErr, actualErr)
	if expectedErr == nil {
		assertIteratorsEqual(t, expectedCurrentStakerIterator, actualCurrentStakerIterator)
	}

	expectedPendingStakerIterator, expectedErr := expected.GetPendingStakerIterator()
	actualPendingStakerIterator, actualErr := actual.GetPendingStakerIterator()
	assert.Equal(t, expectedErr, actualErr)
	if expectedErr == nil {
		assertIteratorsEqual(t, expectedPendingStakerIterator, actualPendingStakerIterator)
	}

	assert.Equal(t, expected.GetTimestamp(), actual.GetTimestamp())
	assert.Equal(t, expected.GetCurrentSupply(), actual.GetCurrentSupply())

	expectedSubnets, expectedErr := expected.GetSubnets()
	actualSubnets, actualErr := actual.GetSubnets()
	assert.Equal(t, expectedErr, actualErr)
	if expectedErr == nil {
		assert.Equal(t, expectedSubnets, actualSubnets)

		for _, subnet := range expectedSubnets {
			subnetID := subnet.ID()

			expectedChains, expectedErr := expected.GetChains(subnetID)
			actualChains, actualErr := actual.GetChains(subnetID)
			assert.Equal(t, expectedErr, actualErr)
			if expectedErr == nil {
				assert.Equal(t, expectedChains, actualChains)
			}
		}
	}
}

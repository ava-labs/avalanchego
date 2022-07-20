// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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

func TestDiffCurrentValidator(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lastAcceptedID := ids.GenerateTestID()
	state := NewMockState(ctrl)
	// Called in NewDiff
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	state.EXPECT().GetCurrentSupply().Return(uint64(1337)).Times(1)

	states := NewMockVersions(ctrl)
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	d, err := NewDiff(lastAcceptedID, states)
	assert.NoError(err)

	// Put a current validator
	currentValidator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}
	d.PutCurrentValidator(currentValidator)

	// Assert that we get the current validator back
	gotCurrentValidator, err := d.GetCurrentValidator(currentValidator.SubnetID, currentValidator.NodeID)
	assert.NoError(err)
	assert.Equal(currentValidator, gotCurrentValidator)

	// Delete the current validator
	d.DeleteCurrentValidator(currentValidator)

	// Make sure the deletion worked
	_, err = d.GetCurrentValidator(currentValidator.SubnetID, currentValidator.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)
}

func TestDiffPendingValidator(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lastAcceptedID := ids.GenerateTestID()
	state := NewMockState(ctrl)
	// Called in NewDiff
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	state.EXPECT().GetCurrentSupply().Return(uint64(1337)).Times(1)

	states := NewMockVersions(ctrl)
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	d, err := NewDiff(lastAcceptedID, states)
	assert.NoError(err)

	// Put a pending validator
	pendingValidator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}
	d.PutPendingValidator(pendingValidator)

	// Assert that we get the pending validator back
	gotPendingValidator, err := d.GetPendingValidator(pendingValidator.SubnetID, pendingValidator.NodeID)
	assert.NoError(err)
	assert.Equal(pendingValidator, gotPendingValidator)

	// Delete the pending validator
	d.DeletePendingValidator(pendingValidator)

	// Make sure the deletion worked
	_, err = d.GetPendingValidator(pendingValidator.SubnetID, pendingValidator.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)
}

func TestDiffCurrentDelegator(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	currentDelegator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}

	state := NewMockState(ctrl)
	// Called in NewDiff
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	state.EXPECT().GetCurrentSupply().Return(uint64(1337)).Times(1)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	d, err := NewDiff(lastAcceptedID, states)
	assert.NoError(err)

	// Put a current delegator
	d.PutCurrentDelegator(currentDelegator)

	// Assert that we get the current delegator back
	// Mock iterator for [state] returns no delegators.
	stateCurrentDelegatorIter := NewMockStakerIterator(ctrl)
	stateCurrentDelegatorIter.EXPECT().Next().Return(false).Times(2)
	stateCurrentDelegatorIter.EXPECT().Release().Times(2)
	state.EXPECT().GetCurrentDelegatorIterator(
		currentDelegator.SubnetID,
		currentDelegator.NodeID,
	).Return(stateCurrentDelegatorIter, nil).Times(2)
	gotCurrentDelegatorIter, err := d.GetCurrentDelegatorIterator(currentDelegator.SubnetID, currentDelegator.NodeID)
	assert.NoError(err)
	// The iterator should have the 1 delegator we put in [d]
	assert.True(gotCurrentDelegatorIter.Next())
	assert.Equal(gotCurrentDelegatorIter.Value(), currentDelegator)

	// Delete the current delegator
	d.DeleteCurrentDelegator(currentDelegator)

	// Make sure the deletion worked.
	// The iterator should have no elements.
	gotCurrentDelegatorIter, err = d.GetCurrentDelegatorIterator(currentDelegator.SubnetID, currentDelegator.NodeID)
	assert.NoError(err)
	assert.False(gotCurrentDelegatorIter.Next())
}

func TestDiffPendingDelegator(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pendingDelegator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}

	state := NewMockState(ctrl)
	// Called in NewDiff
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	state.EXPECT().GetCurrentSupply().Return(uint64(1337)).Times(1)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	d, err := NewDiff(lastAcceptedID, states)
	assert.NoError(err)

	// Put a pending delegator
	d.PutPendingDelegator(pendingDelegator)

	// Assert that we get the pending delegator back
	// Mock iterator for [state] returns no delegators.
	statePendingDelegatorIter := NewMockStakerIterator(ctrl)
	statePendingDelegatorIter.EXPECT().Next().Return(false).Times(2)
	statePendingDelegatorIter.EXPECT().Release().Times(2)
	state.EXPECT().GetPendingDelegatorIterator(
		pendingDelegator.SubnetID,
		pendingDelegator.NodeID,
	).Return(statePendingDelegatorIter, nil).Times(2)
	gotPendingDelegatorIter, err := d.GetPendingDelegatorIterator(pendingDelegator.SubnetID, pendingDelegator.NodeID)
	assert.NoError(err)
	// The iterator should have the 1 delegator we put in [d]
	assert.True(gotPendingDelegatorIter.Next())
	assert.Equal(gotPendingDelegatorIter.Value(), pendingDelegator)

	// Delete the pending delegator
	d.DeletePendingDelegator(pendingDelegator)

	// Make sure the deletion worked.
	// The iterator should have no elements.
	gotPendingDelegatorIter, err = d.GetPendingDelegatorIterator(pendingDelegator.SubnetID, pendingDelegator.NodeID)
	assert.NoError(err)
	assert.False(gotPendingDelegatorIter.Next())
}

func TestDiffSubnet(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := NewMockState(ctrl)
	// Called in NewDiff
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)
	state.EXPECT().GetCurrentSupply().Return(uint64(1337)).Times(1)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	d, err := NewDiff(lastAcceptedID, states)
	assert.NoError(err)

	// Put a subnet
	createSubnetTx := &txs.Tx{}
	d.AddSubnet(createSubnetTx)

	// Assert that we get the subnet back
	// [state] returns 1 subnet.
	parentStateCreateSubnetTx := &txs.Tx{}
	state.EXPECT().GetSubnets().Return([]*txs.Tx{parentStateCreateSubnetTx}, nil).Times(1)
	gotSubnets, err := d.GetSubnets()
	assert.NoError(err)
	assert.Len(gotSubnets, 2)
	assert.Equal(gotSubnets[0], parentStateCreateSubnetTx)
	assert.Equal(gotSubnets[1], createSubnetTx)
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

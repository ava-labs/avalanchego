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
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
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

func TestDiffChain(t *testing.T) {
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

	// Put a chain
	subnetID := ids.GenerateTestID()
	createChainTx := &txs.Tx{
		Unsigned: &txs.CreateChainTx{
			SubnetID: subnetID,
		},
	}
	d.AddChain(createChainTx)

	// Assert that we get the chain back
	// [state] returns 1 chain.
	parentStateCreateChainTx := &txs.Tx{
		Unsigned: &txs.CreateChainTx{
			SubnetID: subnetID, // note this is the same subnet as [createChainTx]
		},
	}
	state.EXPECT().GetChains(subnetID).Return([]*txs.Tx{parentStateCreateChainTx}, nil).Times(1)
	gotChains, err := d.GetChains(subnetID)
	assert.NoError(err)
	assert.Len(gotChains, 2)
	assert.Equal(gotChains[0], parentStateCreateChainTx)
	assert.Equal(gotChains[1], createChainTx)
}

func TestDiffTx(t *testing.T) {
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

	// Put a tx
	subnetID := ids.GenerateTestID()
	tx := &txs.Tx{
		Unsigned: &txs.CreateChainTx{
			SubnetID: subnetID,
		},
	}
	tx.Initialize(utils.RandomBytes(16), utils.RandomBytes(16))
	d.AddTx(tx, status.Committed)

	{
		// Assert that we get the tx back
		gotTx, gotStatus, err := d.GetTx(tx.ID())
		assert.NoError(err)
		assert.Equal(status.Committed, gotStatus)
		assert.Equal(tx, gotTx)
	}

	{
		// Assert that we can get a tx from the parent state
		// [state] returns 1 tx.
		parentTx := &txs.Tx{
			Unsigned: &txs.CreateChainTx{
				SubnetID: subnetID,
			},
		}
		parentTx.Initialize(utils.RandomBytes(16), utils.RandomBytes(16))
		state.EXPECT().GetTx(parentTx.ID()).Return(parentTx, status.Committed, nil).Times(1)
		gotParentTx, gotStatus, err := d.GetTx(parentTx.ID())
		assert.NoError(err)
		assert.Equal(status.Committed, gotStatus)
		assert.Equal(parentTx, gotParentTx)
	}
}

func TestDiffRewardUTXO(t *testing.T) {
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

	// Put a reward UTXO
	txID := ids.GenerateTestID()
	rewardUTXO := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: txID},
	}
	d.AddRewardUTXO(txID, rewardUTXO)

	{
		// Assert that we get the UTXO back
		gotRewardUTXOs, err := d.GetRewardUTXOs(txID)
		assert.NoError(err)
		assert.Len(gotRewardUTXOs, 1)
		assert.Equal(rewardUTXO, gotRewardUTXOs[0])
	}

	{
		// Assert that we can get a UTXO from the parent state
		// [state] returns 1 UTXO.
		txID2 := ids.GenerateTestID()
		parentRewardUTXO := &avax.UTXO{
			UTXOID: avax.UTXOID{TxID: txID2},
		}
		state.EXPECT().GetRewardUTXOs(txID2).Return([]*avax.UTXO{parentRewardUTXO}, nil).Times(1)
		gotParentRewardUTXOs, err := d.GetRewardUTXOs(txID2)
		assert.NoError(err)
		assert.Len(gotParentRewardUTXOs, 1)
		assert.Equal(parentRewardUTXO, gotParentRewardUTXOs[0])
	}
}

func TestDiffUTXO(t *testing.T) {
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

	// Put a UTXO
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
	}
	d.AddUTXO(utxo)

	{
		// Assert that we get the UTXO back
		gotUTXO, err := d.GetUTXO(utxo.InputID())
		assert.NoError(err)
		assert.Equal(utxo, gotUTXO)
	}

	{
		// Assert that we can get a UTXO from the parent state
		// [state] returns 1 UTXO.
		parentUTXO := &avax.UTXO{
			UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
		}
		state.EXPECT().GetUTXO(parentUTXO.InputID()).Return(parentUTXO, nil).Times(1)
		gotParentUTXO, err := d.GetUTXO(parentUTXO.InputID())
		assert.NoError(err)
		assert.Equal(parentUTXO, gotParentUTXO)
	}

	{
		// Delete the UTXO
		d.DeleteUTXO(utxo.InputID())

		// Make sure it's gone
		_, err = d.GetUTXO(utxo.InputID())
		assert.Error(err)
	}
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

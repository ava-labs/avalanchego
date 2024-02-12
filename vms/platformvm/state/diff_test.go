// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestDiffMissingState(t *testing.T) {
	ctrl := gomock.NewController(t)

	versions := NewMockVersions(ctrl)

	parentID := ids.GenerateTestID()
	versions.EXPECT().GetState(parentID).Times(1).Return(nil, false)

	_, err := NewDiff(parentID, versions)
	require.ErrorIs(t, err, ErrMissingParentState)
}

func TestDiffCreation(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	lastAcceptedID := ids.GenerateTestID()
	state := newInitializedState(require)
	versions := NewMockVersions(ctrl)
	versions.EXPECT().GetState(lastAcceptedID).AnyTimes().Return(state, true)

	d, err := NewDiff(lastAcceptedID, versions)
	require.NoError(err)
	assertChainsEqual(t, state, d)
}

func TestDiffCurrentSupply(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	lastAcceptedID := ids.GenerateTestID()
	state := newInitializedState(require)
	versions := NewMockVersions(ctrl)
	versions.EXPECT().GetState(lastAcceptedID).AnyTimes().Return(state, true)

	d, err := NewDiff(lastAcceptedID, versions)
	require.NoError(err)

	initialCurrentSupply, err := d.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)

	newCurrentSupply := initialCurrentSupply + 1
	d.SetCurrentSupply(constants.PrimaryNetworkID, newCurrentSupply)

	returnedNewCurrentSupply, err := d.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(newCurrentSupply, returnedNewCurrentSupply)

	returnedBaseCurrentSupply, err := state.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(initialCurrentSupply, returnedBaseCurrentSupply)
}

func TestDiffCurrentValidator(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	lastAcceptedID := ids.GenerateTestID()
	state := NewMockState(ctrl)
	// Called in NewDiff
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)

	states := NewMockVersions(ctrl)
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	d, err := NewDiff(lastAcceptedID, states)
	require.NoError(err)

	// Put a current validator
	currentValidator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}
	d.PutCurrentValidator(currentValidator)

	// Assert that we get the current validator back
	gotCurrentValidator, err := d.GetCurrentValidator(currentValidator.SubnetID, currentValidator.NodeID)
	require.NoError(err)
	require.Equal(currentValidator, gotCurrentValidator)

	// Delete the current validator
	d.DeleteCurrentValidator(currentValidator)

	// Make sure the deletion worked
	state.EXPECT().GetCurrentValidator(currentValidator.SubnetID, currentValidator.NodeID).Return(nil, database.ErrNotFound).Times(1)
	_, err = d.GetCurrentValidator(currentValidator.SubnetID, currentValidator.NodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestDiffPendingValidator(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	lastAcceptedID := ids.GenerateTestID()
	state := NewMockState(ctrl)
	// Called in NewDiff
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)

	states := NewMockVersions(ctrl)
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	d, err := NewDiff(lastAcceptedID, states)
	require.NoError(err)

	// Put a pending validator
	pendingValidator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}
	d.PutPendingValidator(pendingValidator)

	// Assert that we get the pending validator back
	gotPendingValidator, err := d.GetPendingValidator(pendingValidator.SubnetID, pendingValidator.NodeID)
	require.NoError(err)
	require.Equal(pendingValidator, gotPendingValidator)

	// Delete the pending validator
	d.DeletePendingValidator(pendingValidator)

	// Make sure the deletion worked
	state.EXPECT().GetPendingValidator(pendingValidator.SubnetID, pendingValidator.NodeID).Return(nil, database.ErrNotFound).Times(1)
	_, err = d.GetPendingValidator(pendingValidator.SubnetID, pendingValidator.NodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestDiffCurrentDelegator(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	currentDelegator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}

	state := NewMockState(ctrl)
	// Called in NewDiff
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	d, err := NewDiff(lastAcceptedID, states)
	require.NoError(err)

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
	require.NoError(err)
	// The iterator should have the 1 delegator we put in [d]
	require.True(gotCurrentDelegatorIter.Next())
	require.Equal(gotCurrentDelegatorIter.Value(), currentDelegator)

	// Delete the current delegator
	d.DeleteCurrentDelegator(currentDelegator)

	// Make sure the deletion worked.
	// The iterator should have no elements.
	gotCurrentDelegatorIter, err = d.GetCurrentDelegatorIterator(currentDelegator.SubnetID, currentDelegator.NodeID)
	require.NoError(err)
	require.False(gotCurrentDelegatorIter.Next())
}

func TestDiffPendingDelegator(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	pendingDelegator := &Staker{
		TxID:     ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
		NodeID:   ids.GenerateTestNodeID(),
	}

	state := NewMockState(ctrl)
	// Called in NewDiff
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	d, err := NewDiff(lastAcceptedID, states)
	require.NoError(err)

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
	require.NoError(err)
	// The iterator should have the 1 delegator we put in [d]
	require.True(gotPendingDelegatorIter.Next())
	require.Equal(gotPendingDelegatorIter.Value(), pendingDelegator)

	// Delete the pending delegator
	d.DeletePendingDelegator(pendingDelegator)

	// Make sure the deletion worked.
	// The iterator should have no elements.
	gotPendingDelegatorIter, err = d.GetPendingDelegatorIterator(pendingDelegator.SubnetID, pendingDelegator.NodeID)
	require.NoError(err)
	require.False(gotPendingDelegatorIter.Next())
}

func TestDiffSubnet(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := newInitializedState(require)

	// Initialize parent with one subnet
	parentStateCreateSubnetTx := &txs.Tx{
		Unsigned: &txs.CreateSubnetTx{
			Owner: fx.NewMockOwner(ctrl),
		},
	}
	state.AddSubnet(parentStateCreateSubnetTx)

	// Verify parent returns one subnet
	subnets, err := state.GetSubnets()
	require.NoError(err)
	require.Equal([]*txs.Tx{
		parentStateCreateSubnetTx,
	}, subnets)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	diff, err := NewDiff(lastAcceptedID, states)
	require.NoError(err)

	// Put a subnet
	createSubnetTx := &txs.Tx{
		Unsigned: &txs.CreateSubnetTx{
			Owner: fx.NewMockOwner(ctrl),
		},
	}
	diff.AddSubnet(createSubnetTx)

	// Apply diff to parent state
	require.NoError(diff.Apply(state))

	// Verify parent now returns two subnets
	subnets, err = state.GetSubnets()
	require.NoError(err)
	require.Equal([]*txs.Tx{
		parentStateCreateSubnetTx,
		createSubnetTx,
	}, subnets)
}

func TestDiffChain(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := newInitializedState(require)
	subnetID := ids.GenerateTestID()

	// Initialize parent with one chain
	parentStateCreateChainTx := &txs.Tx{
		Unsigned: &txs.CreateChainTx{
			SubnetID: subnetID,
		},
	}
	state.AddChain(parentStateCreateChainTx)

	// Verify parent returns one chain
	chains, err := state.GetChains(subnetID)
	require.NoError(err)
	require.Equal([]*txs.Tx{
		parentStateCreateChainTx,
	}, chains)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	diff, err := NewDiff(lastAcceptedID, states)
	require.NoError(err)

	// Put a chain
	createChainTx := &txs.Tx{
		Unsigned: &txs.CreateChainTx{
			SubnetID: subnetID, // note this is the same subnet as [parentStateCreateChainTx]
		},
	}
	diff.AddChain(createChainTx)

	// Apply diff to parent state
	require.NoError(diff.Apply(state))

	// Verify parent now returns two chains
	chains, err = state.GetChains(subnetID)
	require.NoError(err)
	require.Equal([]*txs.Tx{
		parentStateCreateChainTx,
		createChainTx,
	}, chains)
}

func TestDiffTx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := NewMockState(ctrl)
	// Called in NewDiff
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	d, err := NewDiff(lastAcceptedID, states)
	require.NoError(err)

	// Put a tx
	subnetID := ids.GenerateTestID()
	tx := &txs.Tx{
		Unsigned: &txs.CreateChainTx{
			SubnetID: subnetID,
		},
	}
	tx.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))
	d.AddTx(tx, status.Committed)

	{
		// Assert that we get the tx back
		gotTx, gotStatus, err := d.GetTx(tx.ID())
		require.NoError(err)
		require.Equal(status.Committed, gotStatus)
		require.Equal(tx, gotTx)
	}

	{
		// Assert that we can get a tx from the parent state
		// [state] returns 1 tx.
		parentTx := &txs.Tx{
			Unsigned: &txs.CreateChainTx{
				SubnetID: subnetID,
			},
		}
		parentTx.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))
		state.EXPECT().GetTx(parentTx.ID()).Return(parentTx, status.Committed, nil).Times(1)
		gotParentTx, gotStatus, err := d.GetTx(parentTx.ID())
		require.NoError(err)
		require.Equal(status.Committed, gotStatus)
		require.Equal(parentTx, gotParentTx)
	}
}

func TestDiffRewardUTXO(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := newInitializedState(require)

	txID := ids.GenerateTestID()

	// Initialize parent with one reward UTXO
	parentRewardUTXO := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: txID},
	}
	state.AddRewardUTXO(txID, parentRewardUTXO)

	// Verify parent returns the reward UTXO
	rewardUTXOs, err := state.GetRewardUTXOs(txID)
	require.NoError(err)
	require.Equal([]*avax.UTXO{
		parentRewardUTXO,
	}, rewardUTXOs)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	diff, err := NewDiff(lastAcceptedID, states)
	require.NoError(err)

	// Put a reward UTXO
	rewardUTXO := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: txID},
	}
	diff.AddRewardUTXO(txID, rewardUTXO)

	// Apply diff to parent state
	require.NoError(diff.Apply(state))

	// Verify parent now returns two reward UTXOs
	rewardUTXOs, err = state.GetRewardUTXOs(txID)
	require.NoError(err)
	require.Equal([]*avax.UTXO{
		parentRewardUTXO,
		rewardUTXO,
	}, rewardUTXOs)
}

func TestDiffUTXO(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := NewMockState(ctrl)
	// Called in NewDiff
	state.EXPECT().GetTimestamp().Return(time.Now()).Times(1)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	d, err := NewDiff(lastAcceptedID, states)
	require.NoError(err)

	// Put a UTXO
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
	}
	d.AddUTXO(utxo)

	{
		// Assert that we get the UTXO back
		gotUTXO, err := d.GetUTXO(utxo.InputID())
		require.NoError(err)
		require.Equal(utxo, gotUTXO)
	}

	{
		// Assert that we can get a UTXO from the parent state
		// [state] returns 1 UTXO.
		parentUTXO := &avax.UTXO{
			UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
		}
		state.EXPECT().GetUTXO(parentUTXO.InputID()).Return(parentUTXO, nil).Times(1)
		gotParentUTXO, err := d.GetUTXO(parentUTXO.InputID())
		require.NoError(err)
		require.Equal(parentUTXO, gotParentUTXO)
	}

	{
		// Delete the UTXO
		d.DeleteUTXO(utxo.InputID())

		// Make sure it's gone
		_, err = d.GetUTXO(utxo.InputID())
		require.ErrorIs(err, database.ErrNotFound)
	}
}

func assertChainsEqual(t *testing.T, expected, actual Chain) {
	require := require.New(t)

	t.Helper()

	expectedCurrentStakerIterator, expectedErr := expected.GetCurrentStakerIterator()
	actualCurrentStakerIterator, actualErr := actual.GetCurrentStakerIterator()
	require.Equal(expectedErr, actualErr)
	if expectedErr == nil {
		assertIteratorsEqual(t, expectedCurrentStakerIterator, actualCurrentStakerIterator)
	}

	expectedPendingStakerIterator, expectedErr := expected.GetPendingStakerIterator()
	actualPendingStakerIterator, actualErr := actual.GetPendingStakerIterator()
	require.Equal(expectedErr, actualErr)
	if expectedErr == nil {
		assertIteratorsEqual(t, expectedPendingStakerIterator, actualPendingStakerIterator)
	}

	require.Equal(expected.GetTimestamp(), actual.GetTimestamp())

	expectedCurrentSupply, err := expected.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)

	actualCurrentSupply, err := actual.GetCurrentSupply(constants.PrimaryNetworkID)
	require.NoError(err)

	require.Equal(expectedCurrentSupply, actualCurrentSupply)
}

func TestDiffSubnetOwner(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := newInitializedState(require)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	var (
		owner1 = fx.NewMockOwner(ctrl)
		owner2 = fx.NewMockOwner(ctrl)

		createSubnetTx = &txs.Tx{
			Unsigned: &txs.CreateSubnetTx{
				BaseTx: txs.BaseTx{},
				Owner:  owner1,
			},
		}

		subnetID = createSubnetTx.ID()
	)

	// Create subnet on base state
	owner, err := state.GetSubnetOwner(subnetID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(owner)

	state.AddSubnet(createSubnetTx)
	state.SetSubnetOwner(subnetID, owner1)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Create diff and verify that subnet owner returns correctly
	d, err := NewDiff(lastAcceptedID, states)
	require.NoError(err)

	owner, err = d.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Transferring subnet ownership on diff should be reflected on diff not state
	d.SetSubnetOwner(subnetID, owner2)
	owner, err = d.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// State should reflect new subnet owner after diff is applied.
	require.NoError(d.Apply(state))

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)
}

func TestDiffStacking(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := newInitializedState(require)

	states := NewMockVersions(ctrl)
	lastAcceptedID := ids.GenerateTestID()
	states.EXPECT().GetState(lastAcceptedID).Return(state, true).AnyTimes()

	var (
		owner1 = fx.NewMockOwner(ctrl)
		owner2 = fx.NewMockOwner(ctrl)
		owner3 = fx.NewMockOwner(ctrl)

		createSubnetTx = &txs.Tx{
			Unsigned: &txs.CreateSubnetTx{
				BaseTx: txs.BaseTx{},
				Owner:  owner1,
			},
		}

		subnetID = createSubnetTx.ID()
	)

	// Create subnet on base state
	owner, err := state.GetSubnetOwner(subnetID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(owner)

	state.AddSubnet(createSubnetTx)
	state.SetSubnetOwner(subnetID, owner1)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Create first diff and verify that subnet owner returns correctly
	statesDiff, err := NewDiff(lastAcceptedID, states)
	require.NoError(err)

	owner, err = statesDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Transferring subnet ownership on first diff should be reflected on first diff not state
	statesDiff.SetSubnetOwner(subnetID, owner2)
	owner, err = statesDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Create a second diff on first diff and verify that subnet owner returns correctly
	stackedDiff, err := NewDiffOn(statesDiff)
	require.NoError(err)
	owner, err = stackedDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)

	// Transfer ownership on stacked diff and verify it is only reflected on stacked diff
	stackedDiff.SetSubnetOwner(subnetID, owner3)
	owner, err = stackedDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner3, owner)

	owner, err = statesDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	// Applying both diffs successively should work as expected.
	require.NoError(stackedDiff.Apply(statesDiff))

	owner, err = statesDiff.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner3, owner)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	require.NoError(statesDiff.Apply(state))

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner3, owner)
}

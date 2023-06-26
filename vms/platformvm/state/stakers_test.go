// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestBaseStakersPruning(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()
	delegator := newTestStaker()
	delegator.SubnetID = staker.SubnetID
	delegator.NodeID = staker.NodeID

	v := newBaseStakers()

	v.PutValidator(staker)

	_, err := v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)

	v.PutDelegator(delegator)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)

	v.DeleteValidator(staker)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	v.DeleteDelegator(delegator)

	require.Empty(v.validators)

	v.PutValidator(staker)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)

	v.PutDelegator(delegator)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)

	v.DeleteDelegator(delegator)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)

	v.DeleteValidator(staker)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	require.Empty(v.validators)
}

func TestBaseStakersValidator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()
	delegator := newTestStaker()

	v := newBaseStakers()

	v.PutDelegator(delegator)

	_, err := v.GetValidator(ids.GenerateTestID(), delegator.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = v.GetValidator(delegator.SubnetID, ids.GenerateTestNodeID())
	require.ErrorIs(err, database.ErrNotFound)

	_, err = v.GetValidator(delegator.SubnetID, delegator.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	stakerIterator := v.GetStakerIterator()
	assertIteratorsEqual(t, NewSliceIterator(delegator), stakerIterator)

	v.PutValidator(staker)

	returnedStaker, err := v.GetValidator(staker.SubnetID, staker.NodeID)
	require.NoError(err)
	require.Equal(staker, returnedStaker)

	v.DeleteDelegator(delegator)

	stakerIterator = v.GetStakerIterator()
	assertIteratorsEqual(t, NewSliceIterator(staker), stakerIterator)

	v.DeleteValidator(staker)

	_, err = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	stakerIterator = v.GetStakerIterator()
	assertIteratorsEqual(t, EmptyIterator, stakerIterator)
}

func TestBaseStakersDelegator(t *testing.T) {
	staker := newTestStaker()
	delegator := newTestStaker()

	v := newBaseStakers()

	delegatorIterator := v.GetDelegatorIterator(delegator.SubnetID, delegator.NodeID)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)

	v.PutDelegator(delegator)

	delegatorIterator = v.GetDelegatorIterator(delegator.SubnetID, ids.GenerateTestNodeID())
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)

	delegatorIterator = v.GetDelegatorIterator(delegator.SubnetID, delegator.NodeID)
	assertIteratorsEqual(t, NewSliceIterator(delegator), delegatorIterator)

	v.DeleteDelegator(delegator)

	delegatorIterator = v.GetDelegatorIterator(delegator.SubnetID, delegator.NodeID)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)

	v.PutValidator(staker)

	v.PutDelegator(delegator)
	v.DeleteDelegator(delegator)

	delegatorIterator = v.GetDelegatorIterator(staker.SubnetID, staker.NodeID)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)
}

func TestDiffStakersValidator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()
	delegator := newTestStaker()

	v := diffStakers{}

	v.PutDelegator(delegator)

	// validators not available in the diff are marked as unmodified
	_, status := v.GetValidator(ids.GenerateTestID(), delegator.NodeID)
	require.Equal(unmodified, status)

	_, status = v.GetValidator(delegator.SubnetID, ids.GenerateTestNodeID())
	require.Equal(unmodified, status)

	// delegator addition shouldn't change validatorStatus
	_, status = v.GetValidator(delegator.SubnetID, delegator.NodeID)
	require.Equal(unmodified, status)

	stakerIterator := v.GetStakerIterator(EmptyIterator)
	assertIteratorsEqual(t, NewSliceIterator(delegator), stakerIterator)

	v.PutValidator(staker)

	returnedStaker, status := v.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(added, status)
	require.Equal(staker, returnedStaker)

	v.DeleteValidator(staker)

	// Validators created and deleted in the same diff are marked as unmodified.
	// This means they won't be pushed to baseState if diff.Apply(baseState) is
	// called.
	_, status = v.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(unmodified, status)

	stakerIterator = v.GetStakerIterator(EmptyIterator)
	assertIteratorsEqual(t, NewSliceIterator(delegator), stakerIterator)
}

func TestDiffStakersDeleteValidator(t *testing.T) {
	require := require.New(t)
	staker := newTestStaker()
	delegator := newTestStaker()

	v := diffStakers{}

	_, status := v.GetValidator(ids.GenerateTestID(), delegator.NodeID)
	require.Equal(unmodified, status)

	v.DeleteValidator(staker)

	returnedStaker, status := v.GetValidator(staker.SubnetID, staker.NodeID)
	require.Equal(deleted, status)
	require.Nil(returnedStaker)
}

func TestDiffStakersDelegator(t *testing.T) {
	staker := newTestStaker()
	delegator := newTestStaker()

	v := diffStakers{}

	v.PutValidator(staker)

	delegatorIterator := v.GetDelegatorIterator(EmptyIterator, ids.GenerateTestID(), delegator.NodeID)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)

	v.PutDelegator(delegator)

	delegatorIterator = v.GetDelegatorIterator(EmptyIterator, delegator.SubnetID, delegator.NodeID)
	assertIteratorsEqual(t, NewSliceIterator(delegator), delegatorIterator)

	v.DeleteDelegator(delegator)

	delegatorIterator = v.GetDelegatorIterator(EmptyIterator, ids.GenerateTestID(), delegator.NodeID)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)
}

func newTestStaker() *Staker {
	startTime := time.Now().Round(time.Second)
	endTime := startTime.Add(28 * 24 * time.Hour)
	return &Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          ids.GenerateTestNodeID(),
		SubnetID:        ids.GenerateTestID(),
		Weight:          1,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: 1,

		NextTime: endTime,
		Priority: txs.PrimaryNetworkDelegatorCurrentPriority,
	}
}

func assertIteratorsEqual(t *testing.T, expected, actual StakerIterator) {
	t.Helper()

	for expected.Next() {
		require.True(t, actual.Next())

		expectedStaker := expected.Value()
		actualStaker := actual.Value()

		require.Equal(t, expectedStaker, actualStaker)
	}
	require.False(t, actual.Next())

	expected.Release()
	actual.Release()
}

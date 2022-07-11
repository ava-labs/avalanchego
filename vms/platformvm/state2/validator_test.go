// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

func TestBaseValidatorsPruning(t *testing.T) {
	assert := assert.New(t)

	staker := newTestStaker()
	delegator := newTestStaker()
	delegator.SubnetID = staker.SubnetID
	delegator.NodeID = staker.NodeID

	v := newBaseValidators()

	v.PutStaker(staker)

	_, err := v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.NoError(err)

	v.PutDelegator(delegator)

	_, err = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.NoError(err)

	v.DeleteStaker(staker)

	_, err = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	v.DeleteDelegator(delegator)

	assert.Empty(v.validators)

	v.PutStaker(staker)

	_, err = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.NoError(err)

	v.PutDelegator(delegator)

	_, err = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.NoError(err)

	v.DeleteDelegator(delegator)

	_, err = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.NoError(err)

	v.DeleteStaker(staker)

	_, err = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	assert.Empty(v.validators)
}

func TestBaseValidatorsStaker(t *testing.T) {
	assert := assert.New(t)

	staker := newTestStaker()
	delegator := newTestStaker()

	v := newBaseValidators()

	v.PutDelegator(delegator)

	_, err := v.GetStaker(ids.GenerateTestID(), delegator.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	_, err = v.GetStaker(delegator.SubnetID, ids.GenerateTestNodeID())
	assert.ErrorIs(err, database.ErrNotFound)

	_, err = v.GetStaker(delegator.SubnetID, delegator.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	stakerIterator := v.GetStakerIterator()
	assertIteratorsEqual(t, NewSliceIterator(delegator), stakerIterator)

	v.PutStaker(staker)

	returnedStaker, err := v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.NoError(err)
	assert.Equal(staker, returnedStaker)

	v.DeleteDelegator(delegator)

	stakerIterator = v.GetStakerIterator()
	assertIteratorsEqual(t, NewSliceIterator(staker), stakerIterator)

	v.DeleteStaker(staker)

	_, err = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	stakerIterator = v.GetStakerIterator()
	assertIteratorsEqual(t, EmptyIterator, stakerIterator)
}

func TestBaseValidatorsDelegator(t *testing.T) {
	staker := newTestStaker()
	delegator := newTestStaker()

	v := newBaseValidators()

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

	v.PutStaker(staker)

	v.PutDelegator(delegator)
	v.DeleteDelegator(delegator)

	delegatorIterator = v.GetDelegatorIterator(staker.SubnetID, staker.NodeID)
	assertIteratorsEqual(t, EmptyIterator, delegatorIterator)
}

func TestDiffValidatorsStaker(t *testing.T) {
	assert := assert.New(t)

	staker := newTestStaker()
	delegator := newTestStaker()

	v := diffValidators{}

	v.PutDelegator(delegator)

	_, ok := v.GetStaker(ids.GenerateTestID(), delegator.NodeID)
	assert.False(ok)

	_, ok = v.GetStaker(delegator.SubnetID, ids.GenerateTestNodeID())
	assert.False(ok)

	_, ok = v.GetStaker(delegator.SubnetID, delegator.NodeID)
	assert.False(ok)

	stakerIterator := v.GetStakerIterator(EmptyIterator)
	assertIteratorsEqual(t, NewSliceIterator(delegator), stakerIterator)

	v.PutStaker(staker)

	returnedStaker, ok := v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.True(ok)
	assert.Equal(staker, returnedStaker)

	v.DeleteStaker(staker)

	returnedStaker, ok = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.True(ok)
	assert.Nil(returnedStaker)

	stakerIterator = v.GetStakerIterator(EmptyIterator)
	assertIteratorsEqual(t, NewSliceIterator(delegator), stakerIterator)
}

func TestDiffValidatorsDelegator(t *testing.T) {
	staker := newTestStaker()
	delegator := newTestStaker()

	v := diffValidators{}

	v.PutStaker(staker)

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
		Priority: PrimaryNetworkDelegatorCurrentPriority,
	}
}

func assertIteratorsEqual(t *testing.T, expected, actual StakerIterator) {
	t.Helper()

	for expected.Next() {
		assert.True(t, actual.Next())

		expectedStaker := expected.Value()
		actualStaker := actual.Value()

		assert.Equal(t, expectedStaker, actualStaker)
	}
	assert.False(t, actual.Next())

	expected.Release()
	actual.Release()
}

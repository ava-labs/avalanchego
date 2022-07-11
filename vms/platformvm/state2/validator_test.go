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

func TestBaseValidatorsStaker(t *testing.T) {
	assert := assert.New(t)

	staker := newTestStaker()

	v := newBaseValidators()

	v.PutDelegator(staker)

	_, err := v.GetStaker(ids.GenerateTestID(), staker.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	_, err = v.GetStaker(staker.SubnetID, ids.GenerateTestNodeID())
	assert.ErrorIs(err, database.ErrNotFound)

	_, err = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	v.PutStaker(staker)

	returnedStaker, err := v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.NoError(err)
	assert.Equal(staker, returnedStaker)

	stakerIterator := v.GetStakerIterator()
	assertIteratorsEqual(assert, NewSliceIterator(staker), stakerIterator)

	v.DeleteStaker(staker)

	_, err = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	stakerIterator = v.GetStakerIterator()
	assertIteratorsEqual(assert, EmptyIterator, stakerIterator)
}

func TestBaseValidatorsDelegator(t *testing.T) {
	assert := assert.New(t)

	staker := newTestStaker()

	v := newBaseValidators()

	v.PutDelegator(staker)

	_, err := v.GetStaker(ids.GenerateTestID(), staker.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	_, err = v.GetStaker(staker.SubnetID, ids.GenerateTestNodeID())
	assert.ErrorIs(err, database.ErrNotFound)

	_, err = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	v.PutStaker(staker)

	returnedStaker, err := v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.NoError(err)
	assert.Equal(staker, returnedStaker)

	stakerIterator := v.GetStakerIterator()
	assertIteratorsEqual(assert, NewSliceIterator(staker), stakerIterator)

	v.DeleteStaker(staker)

	_, err = v.GetStaker(staker.SubnetID, staker.NodeID)
	assert.ErrorIs(err, database.ErrNotFound)

	stakerIterator = v.GetStakerIterator()
	assertIteratorsEqual(assert, EmptyIterator, stakerIterator)
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

func assertIteratorsEqual(assert *assert.Assertions, expected, actual StakerIterator) {
	for expected.Next() {
		assert.True(actual.Next())

		expectedStaker := expected.Value()
		actualStaker := actual.Value()

		assert.Equal(expectedStaker, actualStaker)
	}
	assert.False(actual.Next())

	expected.Release()
	actual.Release()
}

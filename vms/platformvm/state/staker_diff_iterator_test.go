// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestStakerDiffIterator(t *testing.T) {
	assert := assert.New(t)
	currentStakers := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(10, 0),
			NextTime: time.Unix(10, 0),
			Priority: PrimaryNetworkValidatorCurrentPriority,
		},
	}
	pendingStakers := []*Staker{
		{
			TxID:      ids.GenerateTestID(),
			StartTime: time.Unix(0, 0),
			EndTime:   time.Unix(5, 0),
			NextTime:  time.Unix(0, 0),
			Priority:  PrimaryNetworkDelegatorPendingPriority,
		},
		{
			TxID:      ids.GenerateTestID(),
			StartTime: time.Unix(5, 0),
			EndTime:   time.Unix(10, 0),
			NextTime:  time.Unix(5, 0),
			Priority:  PrimaryNetworkDelegatorPendingPriority,
		},
		{
			TxID:      ids.GenerateTestID(),
			StartTime: time.Unix(11, 0),
			EndTime:   time.Unix(20, 0),
			NextTime:  time.Unix(11, 0),
			Priority:  PrimaryNetworkValidatorPendingPriority,
		},
		{
			TxID:      ids.GenerateTestID(),
			StartTime: time.Unix(11, 0),
			EndTime:   time.Unix(20, 0),
			NextTime:  time.Unix(11, 0),
			Priority:  PrimaryNetworkDelegatorPendingPriority,
		},
	}

	stakerDiffs := []struct {
		txID    ids.ID
		isAdded bool
	}{
		{
			txID:    pendingStakers[0].TxID,
			isAdded: true,
		},
		{
			txID:    pendingStakers[1].TxID,
			isAdded: true,
		},
		{
			txID:    pendingStakers[0].TxID,
			isAdded: false,
		},
		{
			txID:    pendingStakers[1].TxID,
			isAdded: false,
		},
		{
			txID:    currentStakers[0].TxID,
			isAdded: false,
		},
		{
			txID:    pendingStakers[2].TxID,
			isAdded: true,
		},
		{
			txID:    pendingStakers[3].TxID,
			isAdded: true,
		},
		{
			txID:    pendingStakers[3].TxID,
			isAdded: false,
		},
		{
			txID:    pendingStakers[2].TxID,
			isAdded: false,
		},
	}

	it := NewStakerDiffIterator(
		NewSliceIterator(currentStakers...),
		NewSliceIterator(pendingStakers...),
	)
	for _, expectedStaker := range stakerDiffs {
		assert.True(it.Next())
		staker, isAdded := it.Value()
		assert.Equal(expectedStaker.txID, staker.TxID)
		assert.Equal(expectedStaker.isAdded, isAdded)
	}
	assert.False(it.Next())
	it.Release()
	assert.False(it.Next())
}

func TestMutableStakerIterator(t *testing.T) {
	assert := assert.New(t)
	initialStakers := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(10, 0),
			NextTime: time.Unix(10, 0),
			Priority: PrimaryNetworkValidatorCurrentPriority,
		},
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(20, 0),
			NextTime: time.Unix(20, 0),
			Priority: PrimaryNetworkValidatorCurrentPriority,
		},
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(30, 0),
			NextTime: time.Unix(30, 0),
			Priority: PrimaryNetworkValidatorCurrentPriority,
		},
	}

	it := newMutableStakerIterator(NewSliceIterator(initialStakers...))

	addedStakers := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(5, 0),
			NextTime: time.Unix(5, 0),
			Priority: PrimaryNetworkValidatorCurrentPriority,
		},
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(15, 0),
			NextTime: time.Unix(15, 0),
			Priority: PrimaryNetworkValidatorCurrentPriority,
		},
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(25, 0),
			NextTime: time.Unix(25, 0),
			Priority: PrimaryNetworkValidatorCurrentPriority,
		},
	}

	// Next must be called before Add.
	hasNext := it.Next()
	assert.True(hasNext)

	for _, staker := range addedStakers {
		it.Add(staker)
	}

	stakerDiffs := []ids.ID{
		addedStakers[0].TxID,
		initialStakers[0].TxID,
		addedStakers[1].TxID,
		initialStakers[1].TxID,
		addedStakers[2].TxID,
		initialStakers[2].TxID,
	}

	for _, expectedStakerTxID := range stakerDiffs {
		assert.True(hasNext)
		staker := it.Value()
		assert.Equal(expectedStakerTxID, staker.TxID)
		hasNext = it.Next()
	}
	assert.False(hasNext)
	it.Release()
	assert.False(it.Next())
}

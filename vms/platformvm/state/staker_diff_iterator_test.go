// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestStakerDiffIterator(t *testing.T) {
	require := require.New(t)
	currentStakers := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(10, 0),
			NextTime: time.Unix(10, 0),
			Priority: txs.PrimaryNetworkValidatorCurrentPriority,
		},
	}
	pendingStakers := []*Staker{
		{
			TxID:      ids.GenerateTestID(),
			StartTime: time.Unix(0, 0),
			EndTime:   time.Unix(5, 0),
			NextTime:  time.Unix(0, 0),
			Priority:  txs.PrimaryNetworkDelegatorApricotPendingPriority,
		},
		{
			TxID:      ids.GenerateTestID(),
			StartTime: time.Unix(5, 0),
			EndTime:   time.Unix(10, 0),
			NextTime:  time.Unix(5, 0),
			Priority:  txs.PrimaryNetworkDelegatorApricotPendingPriority,
		},
		{
			TxID:      ids.GenerateTestID(),
			StartTime: time.Unix(11, 0),
			EndTime:   time.Unix(20, 0),
			NextTime:  time.Unix(11, 0),
			Priority:  txs.PrimaryNetworkValidatorPendingPriority,
		},
		{
			TxID:      ids.GenerateTestID(),
			StartTime: time.Unix(11, 0),
			EndTime:   time.Unix(20, 0),
			NextTime:  time.Unix(11, 0),
			Priority:  txs.PrimaryNetworkDelegatorApricotPendingPriority,
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
		iterator.FromSlice(currentStakers...),
		iterator.FromSlice(pendingStakers...),
	)
	for _, expectedStaker := range stakerDiffs {
		require.True(it.Next())
		staker, isAdded := it.Value()
		require.Equal(expectedStaker.txID, staker.TxID)
		require.Equal(expectedStaker.isAdded, isAdded)
	}
	require.False(it.Next())
	it.Release()
	require.False(it.Next())
}

func TestMutableStakerIterator(t *testing.T) {
	require := require.New(t)
	initialStakers := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(10, 0),
			NextTime: time.Unix(10, 0),
			Priority: txs.PrimaryNetworkValidatorCurrentPriority,
		},
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(20, 0),
			NextTime: time.Unix(20, 0),
			Priority: txs.PrimaryNetworkValidatorCurrentPriority,
		},
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(30, 0),
			NextTime: time.Unix(30, 0),
			Priority: txs.PrimaryNetworkValidatorCurrentPriority,
		},
	}

	it := newMutableStakerIterator(iterator.FromSlice(initialStakers...))

	addedStakers := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(5, 0),
			NextTime: time.Unix(5, 0),
			Priority: txs.PrimaryNetworkValidatorCurrentPriority,
		},
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(15, 0),
			NextTime: time.Unix(15, 0),
			Priority: txs.PrimaryNetworkValidatorCurrentPriority,
		},
		{
			TxID:     ids.GenerateTestID(),
			EndTime:  time.Unix(25, 0),
			NextTime: time.Unix(25, 0),
			Priority: txs.PrimaryNetworkValidatorCurrentPriority,
		},
	}

	// Next must be called before Add.
	hasNext := it.Next()
	require.True(hasNext)

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
		require.True(hasNext)
		staker := it.Value()
		require.Equal(expectedStakerTxID, staker.TxID)
		hasNext = it.Next()
	}
	require.False(hasNext)
	it.Release()
	require.False(it.Next())
}

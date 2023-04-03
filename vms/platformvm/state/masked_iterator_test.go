// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestMaskedIterator(t *testing.T) {
	require := require.New(t)
	stakers := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			Weight:   0, // just to simplify debugging
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			Weight:   10, // just to simplify debugging
			NextTime: time.Unix(10, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			Weight:   20, // just to simplify debugging
			NextTime: time.Unix(20, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			Weight:   30, // just to simplify debugging
			NextTime: time.Unix(30, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			Weight:   40, // just to simplify debugging
			NextTime: time.Unix(40, 0),
		},
	}
	maskedStakers := map[ids.ID]*Staker{
		stakers[1].TxID: stakers[1],
	}

	updatedStaker := *stakers[0]
	updatedStaker.Weight = 50
	updatedStaker.NextTime = time.Unix(50, 0)
	updatedStakers := map[ids.ID]*Staker{
		updatedStaker.TxID: &updatedStaker,
	}

	it := NewMaskedIterator(
		NewSliceIterator(stakers...),
		maskedStakers,
		updatedStakers,
	)

	require.True(it.Next())
	require.Equal(stakers[2], it.Value())

	require.True(it.Next())
	require.Equal(stakers[3], it.Value())

	require.True(it.Next())
	require.Equal(stakers[4], it.Value())

	require.True(it.Next())
	require.Equal(&updatedStaker, it.Value())

	require.False(it.Next())
	it.Release()
	require.False(it.Next())
}

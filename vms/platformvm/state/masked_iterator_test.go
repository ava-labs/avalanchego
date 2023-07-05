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
	}
	maskedStakers := map[ids.ID]*Staker{
		stakers[0].TxID: stakers[0],
		stakers[2].TxID: stakers[2],
		stakers[3].TxID: stakers[3],
	}

	it := NewMaskedIterator(
		NewSliceIterator(stakers[:3]...),
		maskedStakers,
	)

	require.True(it.Next())
	require.Equal(stakers[1], it.Value())

	require.False(it.Next())
	it.Release()
	require.False(it.Next())
}

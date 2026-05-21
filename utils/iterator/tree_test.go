// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator_test

import (
	"testing"
	"time"

	"github.com/google/btree"
	"github.com/stretchr/testify/require"

	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/utils/iterator"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/state"
)

var defaultTreeDegree = 2

func TestTree(t *testing.T) {
	require := require.New(t)
	stakers := []*state.Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(1, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(2, 0),
		},
	}

	tree := btree.NewG(defaultTreeDegree, (*state.Staker).Less)
	for _, staker := range stakers {
		require.Nil(tree.ReplaceOrInsert(staker))
	}

	it := iterator.FromTree(tree)
	for _, staker := range stakers {
		require.True(it.Next())
		require.Equal(staker, it.Value())
	}
	require.False(it.Next())
	it.Release()
}

func TestTreeNil(t *testing.T) {
	it := iterator.FromTree[*state.Staker](nil)
	require.False(t, it.Next())
	it.Release()
}

func TestTreeEarlyRelease(t *testing.T) {
	require := require.New(t)
	stakers := []*state.Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(1, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(2, 0),
		},
	}

	tree := btree.NewG(defaultTreeDegree, (*state.Staker).Less)
	for _, staker := range stakers {
		require.Nil(tree.ReplaceOrInsert(staker))
	}

	it := iterator.FromTree(tree)
	require.True(it.Next())
	it.Release()
	require.False(it.Next())
}

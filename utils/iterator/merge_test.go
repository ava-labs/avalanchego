// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func TestMerge(t *testing.T) {
	type test struct {
		name      string
		iterators []iterator.Iterator[*state.Staker]
		expected  []*state.Staker
	}

	txID := ids.GenerateTestID()
	tests := []test{
		{
			name:      "no iterators",
			iterators: []iterator.Iterator[*state.Staker]{},
			expected:  []*state.Staker{},
		},
		{
			name:      "one empty iterator",
			iterators: []iterator.Iterator[*state.Staker]{iterator.Empty[*state.Staker]{}},
			expected:  []*state.Staker{},
		},
		{
			name:      "multiple empty iterator",
			iterators: []iterator.Iterator[*state.Staker]{iterator.Empty[*state.Staker]{}, iterator.Empty[*state.Staker]{}, iterator.Empty[*state.Staker]{}},
			expected:  []*state.Staker{},
		},
		{
			name:      "mixed empty iterators",
			iterators: []iterator.Iterator[*state.Staker]{iterator.Empty[*state.Staker]{}, iterator.FromSlice[*state.Staker]()},
			expected:  []*state.Staker{},
		},
		{
			name: "single iterator",
			iterators: []iterator.Iterator[*state.Staker]{
				iterator.FromSlice[*state.Staker](
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(0, 0),
					},
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(1, 0),
					},
				),
			},
			expected: []*state.Staker{
				{
					TxID:     txID,
					NextTime: time.Unix(0, 0),
				},
				{
					TxID:     txID,
					NextTime: time.Unix(1, 0),
				},
			},
		},
		{
			name: "multiple iterators",
			iterators: []iterator.Iterator[*state.Staker]{
				iterator.FromSlice[*state.Staker](
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(0, 0),
					},
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(2, 0),
					},
				),
				iterator.FromSlice[*state.Staker](
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(1, 0),
					},
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(3, 0),
					},
				),
			},
			expected: []*state.Staker{
				{
					TxID:     txID,
					NextTime: time.Unix(0, 0),
				},
				{
					TxID:     txID,
					NextTime: time.Unix(1, 0),
				},
				{
					TxID:     txID,
					NextTime: time.Unix(2, 0),
				},
				{
					TxID:     txID,
					NextTime: time.Unix(3, 0),
				},
			},
		},
		{
			name: "multiple iterators different lengths",
			iterators: []iterator.Iterator[*state.Staker]{
				iterator.FromSlice[*state.Staker](
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(0, 0),
					},
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(2, 0),
					},
				),
				iterator.FromSlice[*state.Staker](
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(1, 0),
					},
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(3, 0),
					},
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(4, 0),
					},
					&state.Staker{
						TxID:     txID,
						NextTime: time.Unix(5, 0),
					},
				),
			},
			expected: []*state.Staker{
				{
					TxID:     txID,
					NextTime: time.Unix(0, 0),
				},
				{
					TxID:     txID,
					NextTime: time.Unix(1, 0),
				},
				{
					TxID:     txID,
					NextTime: time.Unix(2, 0),
				},
				{
					TxID:     txID,
					NextTime: time.Unix(3, 0),
				},
				{
					TxID:     txID,
					NextTime: time.Unix(4, 0),
				},
				{
					TxID:     txID,
					NextTime: time.Unix(5, 0),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			it := iterator.Merge[*state.Staker]((*state.Staker).Less, tt.iterators...)
			for _, expected := range tt.expected {
				require.True(it.Next())
				require.Equal(expected, it.Value())
			}
			require.False(it.Next())
			it.Release()
			require.False(it.Next())
		})
	}
}

func TestMergedEarlyRelease(t *testing.T) {
	require := require.New(t)
	stakers0 := []*state.Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(2, 0),
		},
	}

	stakers1 := []*state.Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(1, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(3, 0),
		},
	}

	it := iterator.Merge(
		(*state.Staker).Less,
		iterator.Empty[*state.Staker]{},
		iterator.FromSlice(stakers0...),
		iterator.Empty[*state.Staker]{},
		iterator.FromSlice(stakers1...),
		iterator.Empty[*state.Staker]{},
	)
	require.True(it.Next())
	it.Release()
	require.False(it.Next())
}

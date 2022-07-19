// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestMergedIterator(t *testing.T) {
	type test struct {
		name      string
		iterators []StakerIterator
		expected  []*Staker
	}

	txID := ids.GenerateTestID()
	tests := []test{
		{
			name:      "no iterators",
			iterators: []StakerIterator{},
			expected:  []*Staker{},
		},
		{
			name:      "one empty iterator",
			iterators: []StakerIterator{EmptyIterator},
			expected:  []*Staker{},
		},
		{
			name:      "multiple empty iterator",
			iterators: []StakerIterator{EmptyIterator, EmptyIterator, EmptyIterator},
			expected:  []*Staker{},
		},
		{
			name:      "mixed empty iterators",
			iterators: []StakerIterator{EmptyIterator, NewSliceIterator()},
			expected:  []*Staker{},
		},
		{
			name: "single iterator",
			iterators: []StakerIterator{
				NewSliceIterator(
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(0, 0),
					},
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(1, 0),
					},
				),
			},
			expected: []*Staker{
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
			iterators: []StakerIterator{
				NewSliceIterator(
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(0, 0),
					},
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(2, 0),
					},
				),
				NewSliceIterator(
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(1, 0),
					},
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(3, 0),
					},
				),
			},
			expected: []*Staker{
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
			iterators: []StakerIterator{
				NewSliceIterator(
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(0, 0),
					},
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(2, 0),
					},
				),
				NewSliceIterator(
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(1, 0),
					},
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(3, 0),
					},
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(4, 0),
					},
					&Staker{
						TxID:     txID,
						NextTime: time.Unix(5, 0),
					},
				),
			},
			expected: []*Staker{
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
			assert := assert.New(t)
			it := NewMergedIterator(tt.iterators...)
			for _, expected := range tt.expected {
				assert.True(it.Next())
				assert.Equal(expected, it.Value())
			}
			assert.False(it.Next())
			it.Release()
			assert.False(it.Next())
		})
	}
}

func TestMergedIteratorEarlyRelease(t *testing.T) {
	assert := assert.New(t)
	stakers0 := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(2, 0),
		},
	}

	stakers1 := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(1, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(3, 0),
		},
	}

	it := NewMergedIterator(
		EmptyIterator,
		NewSliceIterator(stakers0...),
		EmptyIterator,
		NewSliceIterator(stakers1...),
		EmptyIterator,
	)
	assert.True(it.Next())
	it.Release()
	assert.False(it.Next())
}

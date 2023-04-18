// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeightedHeapInitialize(t *testing.T) {
	require := require.New(t)

	h := weightedHeap{}

	err := h.Initialize([]uint64{2, 2, 1, 3, 3, 1, 3})
	require.NoError(err)

	expectedOrdering := []int{3, 4, 6, 0, 1, 2, 5}
	for i, elem := range h.heap {
		expected := expectedOrdering[i]
		require.Equal(expected, elem.index)
	}
}

func TestWeightedHeapElementLess(t *testing.T) {
	type test struct {
		name     string
		elt1     weightedHeapElement
		elt2     weightedHeapElement
		expected bool
	}
	tests := []test{
		{
			name:     "all same",
			elt1:     weightedHeapElement{},
			elt2:     weightedHeapElement{},
			expected: false,
		},
		{
			name: "first lower weight",
			elt1: weightedHeapElement{},
			elt2: weightedHeapElement{
				weight: 1,
			},
			expected: false,
		},
		{
			name: "first higher weight",
			elt1: weightedHeapElement{
				weight: 1,
			},
			elt2:     weightedHeapElement{},
			expected: true,
		},
		{
			name: "first higher index",
			elt1: weightedHeapElement{
				index: 1,
			},
			elt2:     weightedHeapElement{},
			expected: false,
		},
		{
			name: "second higher index",
			elt1: weightedHeapElement{},
			elt2: weightedHeapElement{
				index: 1,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			require.Equal(tt.expected, tt.elt1.Less(tt.elt2))
		})
	}
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeightedHeapInitialize(t *testing.T) {
	require := require.New(t)

	h := weightedHeap{}

	require.NoError(h.Initialize([]uint64{2, 2, 1, 3, 3, 1, 3}))

	expectedOrdering := []int{3, 4, 6, 0, 1, 2, 5}
	for i, elem := range h.heap {
		expected := expectedOrdering[i]
		require.Equal(expected, elem.index)
	}
}

func TestWeightedHeapElementCompare(t *testing.T) {
	type test struct {
		name     string
		elt1     weightedHeapElement
		elt2     weightedHeapElement
		expected int
	}
	tests := []test{
		{
			name:     "all same",
			elt1:     weightedHeapElement{},
			elt2:     weightedHeapElement{},
			expected: 0,
		},
		{
			name: "lower weight",
			elt1: weightedHeapElement{},
			elt2: weightedHeapElement{
				weight: 1,
			},
			expected: 1,
		},
		{
			name: "higher index",
			elt1: weightedHeapElement{
				index: 1,
			},
			elt2:     weightedHeapElement{},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.expected, tt.elt1.Compare(tt.elt2))
			require.Equal(-tt.expected, tt.elt2.Compare(tt.elt1))
		})
	}
}

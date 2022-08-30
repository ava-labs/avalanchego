// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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

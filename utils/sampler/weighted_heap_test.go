// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWeightedHeapInitialize(t *testing.T) {
	assert := assert.New(t)

	h := weightedHeap{}

	err := h.Initialize([]uint64{2, 2, 1, 3, 3, 1, 3})
	assert.NoError(err)

	expectedOrdering := []int{3, 4, 6, 0, 1, 2, 5}
	for i, elem := range h.heap {
		expected := expectedOrdering[i]
		assert.Equal(expected, elem.index)
	}
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"cmp"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
)

var (
	_ Weighted                            = (*weightedHeap)(nil)
	_ utils.Sortable[weightedHeapElement] = weightedHeapElement{}
)

type weightedHeapElement struct {
	weight           uint64
	cumulativeWeight uint64
	index            int
}

// Compare the elements. Weight is in decreasing order. Index is in increasing
// order.
func (e weightedHeapElement) Compare(other weightedHeapElement) int {
	// By accounting for the initial index of the weights, this results in a
	// stable sort. We do this rather than using `sort.Stable` because of the
	// reported change in performance of the sort used.
	if weightCmp := cmp.Compare(other.weight, e.weight); weightCmp != 0 {
		return weightCmp
	}
	return cmp.Compare(e.index, other.index)
}

// Sampling is performed by executing a search over a tree of elements in the
// order of their probabilistic occurrence.
//
// Initialization takes O(n * log(n)) time, where n is the number of elements
// that can be sampled.
// Sampling can take up to O(log(n)) time. As the distribution becomes more
// biased, sampling will become faster in expectation.
type weightedHeap struct {
	heap []weightedHeapElement
}

func (s *weightedHeap) Initialize(weights []uint64) error {
	numWeights := len(weights)
	if numWeights <= cap(s.heap) {
		s.heap = s.heap[:numWeights]
	} else {
		s.heap = make([]weightedHeapElement, numWeights)
	}
	for i, weight := range weights {
		s.heap[i] = weightedHeapElement{
			weight:           weight,
			cumulativeWeight: weight,
			index:            i,
		}
	}

	// Optimize so that the most probable values are at the top of the heap
	utils.Sort(s.heap)

	// Initialize the heap
	for i := len(s.heap) - 1; i > 0; i-- {
		// Explicitly performing a shift here allows the compiler to avoid
		// checking for negative numbers, which saves a couple cycles
		parentIndex := (i - 1) >> 1
		newWeight, err := math.Add(
			s.heap[parentIndex].cumulativeWeight,
			s.heap[i].cumulativeWeight,
		)
		if err != nil {
			return err
		}
		s.heap[parentIndex].cumulativeWeight = newWeight
	}

	return nil
}

func (s *weightedHeap) Sample(value uint64) (int, bool) {
	if len(s.heap) == 0 || s.heap[0].cumulativeWeight <= value {
		return 0, false
	}

	index := 0
	for {
		currentElement := s.heap[index]
		currentWeight := currentElement.weight
		if value < currentWeight {
			return currentElement.index, true
		}
		value -= currentWeight

		// We shouldn't return the root, so check the left child
		index = index*2 + 1

		if leftWeight := s.heap[index].cumulativeWeight; leftWeight <= value {
			// If the weight is greater than the left weight, you should move to
			// the right child
			value -= leftWeight
			index++
		}
	}
}

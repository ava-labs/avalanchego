// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"sort"

	safemath "github.com/ava-labs/gecko/utils/math"
)

type weightedHeapElement struct {
	weight           uint64
	cumulativeWeight uint64
	index            int
}

type weightedHeap struct {
	heap []weightedHeapElement
}

func (s *weightedHeap) Initialize(weights []uint64) error {
	if len(weights) > len(s.heap) {
		s.heap = make([]weightedHeapElement, len(weights))
	} else {
		s.heap = s.heap[:len(weights)]
	}
	for i, weight := range weights {
		s.heap[i] = weightedHeapElement{
			weight:           weight,
			cumulativeWeight: weight,
			index:            i,
		}
	}

	// Optimize so that the most probable values are at the top of the heap
	sortWeightedHeap(s.heap)

	// Initialize the heap
	for i := len(s.heap) - 1; i > 0; i-- {
		parentIndex := (i - 1) / 2
		newWeight, err := safemath.Add64(
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

func (s *weightedHeap) Sample(value uint64) (int, error) {
	if len(s.heap) == 0 || s.heap[0].cumulativeWeight <= value {
		return 0, errOutOfRange
	}

	index := 0
	for {
		currentElement := s.heap[index]
		currentWeight := currentElement.weight
		if value < currentWeight {
			return currentElement.index, nil
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

type innerSortWeightedHeap []weightedHeapElement

func (lst innerSortWeightedHeap) Less(i, j int) bool { return lst[i].weight > lst[j].weight }
func (lst innerSortWeightedHeap) Len() int           { return len(lst) }
func (lst innerSortWeightedHeap) Swap(i, j int)      { lst[j], lst[i] = lst[i], lst[j] }
func sortWeightedHeap(heap []weightedHeapElement)    { sort.Sort(innerSortWeightedHeap(heap)) }

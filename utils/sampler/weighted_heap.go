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
	heap         []weightedHeapElement
	currentIndex int
	currentValue uint64
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

func (s *weightedHeap) StartSearch(value uint64) error {
	if len(s.heap) == 0 || s.heap[0].cumulativeWeight <= value {
		return errOutOfRange
	}
	s.currentIndex = 0
	s.currentValue = value
	return nil
}

func (s *weightedHeap) ContinueSearch() (int, bool) {
	currentElement := s.heap[s.currentIndex]
	currentWeight := currentElement.weight
	if s.currentValue < currentWeight {
		return currentElement.index, true
	}
	s.currentValue -= currentWeight

	// We shouldn't return the root, so check the left child
	s.currentIndex = s.currentIndex*2 + 1

	if leftWeight := s.heap[s.currentIndex].cumulativeWeight; leftWeight <= s.currentValue {
		// If the weight is greater than the left weight, you should move to
		// the right child
		s.currentValue -= leftWeight
		s.currentIndex++
	}
	return 0, false
}

type innerSortWeightedHeap []weightedHeapElement

func (lst innerSortWeightedHeap) Less(i, j int) bool { return lst[i].weight > lst[j].weight }
func (lst innerSortWeightedHeap) Len() int           { return len(lst) }
func (lst innerSortWeightedHeap) Swap(i, j int)      { lst[j], lst[i] = lst[i], lst[j] }
func sortWeightedHeap(heap []weightedHeapElement)    { sort.Sort(innerSortWeightedHeap(heap)) }

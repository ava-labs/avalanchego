// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"sort"

	safemath "github.com/ava-labs/gecko/utils/math"
)

type weightedLinearElement struct {
	cumulativeWeight uint64
	index            int
}

type weightedLinear struct {
	arr          []weightedLinearElement
	currentIndex int
	value        uint64
}

func (s *weightedLinear) Initialize(weights []uint64) error {
	if len(weights) > len(s.arr) {
		s.arr = make([]weightedLinearElement, len(weights))
	} else {
		s.arr = s.arr[:len(weights)]
	}

	for i, weight := range weights {
		s.arr[i] = weightedLinearElement{
			cumulativeWeight: weight,
			index:            i,
		}
	}

	// Optimize so that the most probable values are at the front of the array
	sortweightedLinear(s.arr)

	for i := 1; i < len(s.arr); i++ {
		newWeight, err := safemath.Add64(
			s.arr[i-1].cumulativeWeight,
			s.arr[i].cumulativeWeight,
		)
		if err != nil {
			return err
		}
		s.arr[i].cumulativeWeight = newWeight
	}

	return nil
}

func (s *weightedLinear) StartSearch(value uint64) error {
	if len(s.arr) == 0 || s.arr[len(s.arr)-1].cumulativeWeight <= value {
		return errOutOfRange
	}
	s.currentIndex = 0
	s.value = value
	return nil
}

func (s *weightedLinear) ContinueSearch() (int, bool) {
	if currentElement := s.arr[s.currentIndex]; s.value < currentElement.cumulativeWeight {
		return currentElement.index, true
	}
	s.currentIndex++
	return 0, false
}

type innerSortweightedLinear []weightedLinearElement

func (lst innerSortweightedLinear) Less(i, j int) bool {
	return lst[i].cumulativeWeight > lst[j].cumulativeWeight
}
func (lst innerSortweightedLinear) Len() int         { return len(lst) }
func (lst innerSortweightedLinear) Swap(i, j int)    { lst[j], lst[i] = lst[i], lst[j] }
func sortweightedLinear(lst []weightedLinearElement) { sort.Sort(innerSortweightedLinear(lst)) }

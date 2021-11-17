// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"sort"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

type weightedLinearElement struct {
	cumulativeWeight uint64
	index            int
}

// weightedLinear implements the Weighted interface.
//
// Sampling is performed by executing a linear search over the provided elements
// in the order of their probabilistic occurrence.
//
// Initialization takes O(n * log(n)) time, where n is the number of elements
// that can be sampled.
// Sampling can take up to O(n) time. As the distribution becomes more biased,
// sampling will become faster in expectation.
type weightedLinear struct {
	arr []weightedLinearElement
}

func (s *weightedLinear) Initialize(weights []uint64) error {
	numWeights := len(weights)
	if numWeights <= cap(s.arr) {
		s.arr = s.arr[:numWeights]
	} else {
		s.arr = make([]weightedLinearElement, numWeights)
	}

	for i, weight := range weights {
		s.arr[i] = weightedLinearElement{
			cumulativeWeight: weight,
			index:            i,
		}
	}

	// Optimize so that the most probable values are at the front of the array
	sortWeightedLinear(s.arr)

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

func (s *weightedLinear) Sample(value uint64) (int, error) {
	if len(s.arr) == 0 || s.arr[len(s.arr)-1].cumulativeWeight <= value {
		return 0, errOutOfRange
	}

	index := 0
	for {
		if elem := s.arr[index]; value < elem.cumulativeWeight {
			return elem.index, nil
		}
		index++
	}
}

type innerSortWeightedLinear []weightedLinearElement

func (lst innerSortWeightedLinear) Less(i, j int) bool {
	return lst[i].cumulativeWeight > lst[j].cumulativeWeight
}

func (lst innerSortWeightedLinear) Len() int {
	return len(lst)
}

func (lst innerSortWeightedLinear) Swap(i, j int) {
	lst[j], lst[i] = lst[i], lst[j]
}

func sortWeightedLinear(lst []weightedLinearElement) {
	sort.Sort(innerSortWeightedLinear(lst))
}

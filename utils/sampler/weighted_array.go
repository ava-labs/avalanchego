// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"sort"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

type weightedArrayElement struct {
	cumulativeWeight uint64
	index            int
}

// weightedArray implements the Weighted interface.
//
// Sampling is performed by executing a modified binary search over the provided
// elements. Rather than cutting the remaining dataset in half, the algorithm
// attempt to just in to where it think the value will be assuming a linear
// distribution of the element weights.
//
// Initialization takes O(n * log(n)) time, where n is the number of elements
// that can be sampled.
// Sampling can take up to O(n) time. If the distribution is linearly
// distributed, then the runtime is constant.
type weightedArray struct {
	arr []weightedArrayElement
}

func (s *weightedArray) Initialize(weights []uint64) error {
	numWeights := len(weights)
	if numWeights <= cap(s.arr) {
		s.arr = s.arr[:numWeights]
	} else {
		s.arr = make([]weightedArrayElement, numWeights)
	}

	for i, weight := range weights {
		s.arr[i] = weightedArrayElement{
			cumulativeWeight: weight,
			index:            i,
		}
	}

	// Optimize so that the array is closer to the uniform distribution
	sortWeightedArray(s.arr)

	maxIndex := len(s.arr) - 1
	oneIfOdd := 1 & maxIndex
	oneIfEven := 1 - oneIfOdd
	end := maxIndex - oneIfEven
	for i := 1; i < end; i += 2 {
		s.arr[i], s.arr[end] = s.arr[end], s.arr[i]
		end -= 2
	}

	cumulativeWeight := uint64(0)
	for i := 0; i < len(s.arr); i++ {
		newWeight, err := safemath.Add64(
			cumulativeWeight,
			s.arr[i].cumulativeWeight,
		)
		if err != nil {
			return err
		}
		cumulativeWeight = newWeight
		s.arr[i].cumulativeWeight = cumulativeWeight
	}

	return nil
}

func (s *weightedArray) Sample(value uint64) (int, error) {
	if len(s.arr) == 0 || s.arr[len(s.arr)-1].cumulativeWeight <= value {
		return 0, errOutOfRange
	}
	minIndex := 0
	maxIndex := len(s.arr) - 1
	maxCumulativeWeight := float64(s.arr[len(s.arr)-1].cumulativeWeight)
	index := int((float64(value) * float64(maxIndex+1)) / maxCumulativeWeight)

	for {
		previousWeight := uint64(0)
		if index > 0 {
			previousWeight = s.arr[index-1].cumulativeWeight
		}
		currentElem := s.arr[index]
		currentWeight := currentElem.cumulativeWeight
		if previousWeight <= value && value < currentWeight {
			return currentElem.index, nil
		}

		if value < previousWeight {
			// go to the left
			maxIndex = index - 1
		} else {
			// go to the right
			minIndex = index + 1
		}

		minWeight := uint64(0)
		if minIndex > 0 {
			minWeight = s.arr[minIndex-1].cumulativeWeight
		}
		maxWeight := s.arr[maxIndex].cumulativeWeight

		valueRange := maxWeight - minWeight
		adjustedLookupValue := value - minWeight
		indexRange := maxIndex - minIndex + 1
		lookupMass := float64(adjustedLookupValue) * float64(indexRange)

		index = int(lookupMass/float64(valueRange)) + minIndex
	}
}

type innerSortWeightedArray []weightedArrayElement

func (lst innerSortWeightedArray) Less(i, j int) bool {
	return lst[i].cumulativeWeight > lst[j].cumulativeWeight
}

func (lst innerSortWeightedArray) Len() int {
	return len(lst)
}

func (lst innerSortWeightedArray) Swap(i, j int) {
	lst[j], lst[i] = lst[i], lst[j]
}

func sortWeightedArray(lst []weightedArrayElement) {
	sort.Sort(innerSortWeightedArray(lst))
}

// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"sort"

	safemath "github.com/ava-labs/gecko/utils/math"
)

type weightedArrayElement struct {
	cumulativeWeight uint64
	index            int
}

type weightedArray struct {
	arr          []weightedArrayElement
	minIndex     int
	currentIndex int
	maxIndex     int
	value        uint64
}

func (s *weightedArray) Initialize(weights []uint64) error {
	if len(weights) > len(s.arr) {
		s.arr = make([]weightedArrayElement, len(weights))
	} else {
		s.arr = s.arr[:len(weights)]
	}

	for i, weight := range weights {
		s.arr[i] = weightedArrayElement{
			cumulativeWeight: weight,
			index:            i,
		}
	}

	// Optimize so that the array is closer to the uniform distribution
	sortWeightedArray(s.arr)

	arrCopy := make([]weightedArrayElement, len(weights))
	copy(arrCopy, s.arr)

	midpoint := (len(s.arr) + 1) / 2
	for i := 0; i < midpoint; i++ {
		start := 2 * i
		end := len(s.arr) - 1 - i
		s.arr[start] = arrCopy[i]
		if start+1 < len(s.arr) {
			s.arr[start+1] = arrCopy[end]
		}
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
	index := int((float64(value) * float64(s.maxIndex+1)) / float64(s.arr[len(s.arr)-1].cumulativeWeight))

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

		index = int((float64(adjustedLookupValue)*float64(indexRange))/float64(valueRange)) + minIndex
	}
}

type innerSortWeightedArray []weightedArrayElement

func (lst innerSortWeightedArray) Less(i, j int) bool {
	return lst[i].cumulativeWeight > lst[j].cumulativeWeight
}
func (lst innerSortWeightedArray) Len() int        { return len(lst) }
func (lst innerSortWeightedArray) Swap(i, j int)   { lst[j], lst[i] = lst[i], lst[j] }
func sortWeightedArray(lst []weightedArrayElement) { sort.Sort(innerSortWeightedArray(lst)) }

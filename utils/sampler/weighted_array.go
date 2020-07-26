// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	safemath "github.com/ava-labs/gecko/utils/math"
)

type weightedArray struct {
	arr          []uint64
	minIndex     int
	currentIndex int
	maxIndex     int
	value        uint64
}

func (s *weightedArray) Initialize(weights []uint64) error {
	if len(weights) > len(s.arr) {
		s.arr = make([]uint64, len(weights))
	} else {
		s.arr = s.arr[:len(weights)]
	}

	cumulativeWeight := uint64(0)
	for i, weight := range weights {
		newWeight, err := safemath.Add64(
			cumulativeWeight,
			weight,
		)
		if err != nil {
			return err
		}
		cumulativeWeight = newWeight
		s.arr[i] = cumulativeWeight
	}

	return nil
}

func (s *weightedArray) StartSearch(value uint64) error {
	if len(s.arr) == 0 || s.arr[len(s.arr)-1] <= value {
		return errOutOfRange
	}
	s.minIndex = 0
	s.maxIndex = len(s.arr) - 1
	s.value = value
	s.currentIndex = int((float64(value) * float64(s.maxIndex+1)) / float64(s.arr[len(s.arr)-1]))
	return nil
}

func (s *weightedArray) ContinueSearch() (int, bool) {
	previousWeight := uint64(0)
	if s.currentIndex > 0 {
		previousWeight = s.arr[s.currentIndex-1]
	}
	currentWeight := s.arr[s.currentIndex]
	if previousWeight <= s.value && s.value < currentWeight {
		return s.currentIndex, true
	}

	if s.value < previousWeight {
		// go to the left
		s.maxIndex = s.currentIndex - 1
	} else {
		// go to the right
		s.minIndex = s.currentIndex + 1
	}

	minWeight := uint64(0)
	if s.minIndex > 0 {
		minWeight = s.arr[s.minIndex-1]
	}
	maxWeight := s.arr[s.maxIndex]

	valueRange := maxWeight - minWeight
	adjustedLookupValue := s.value - minWeight
	indexRange := s.maxIndex - s.minIndex + 1

	s.currentIndex = int((float64(adjustedLookupValue)*float64(indexRange))/float64(valueRange)) + s.minIndex
	return 0, false
}

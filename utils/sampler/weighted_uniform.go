// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"errors"
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var errWeightsTooLarge = errors.New("total weight is too large")

// weightedUniform implements the Weighted interface.
//
// Sampling is performed by indexing into the array to find the correct index.
//
// Initialization takes O(Sum(weights)) time. This results in an exponential
// initialization time. Therefore, the time to execute this operation can be
// extremely long. Initialization takes O(Sum(weights)) space, causing this
// algorithm to be unable to handle large inputs.
//
// Sampling is performed in O(1) time. However, if the Sum(weights) is large,
// this operation can still be relatively slow due to poor cache locality.
type weightedUniform struct {
	indices   []int
	maxWeight uint64
}

func (s *weightedUniform) Initialize(weights []uint64) error {
	totalWeight := uint64(0)
	for _, weight := range weights {
		newWeight, err := safemath.Add64(totalWeight, weight)
		if err != nil {
			return err
		}
		totalWeight = newWeight
	}
	if totalWeight > s.maxWeight || totalWeight > math.MaxInt32 {
		return errWeightsTooLarge
	}
	size := int(totalWeight)

	if size > cap(s.indices) {
		s.indices = make([]int, size)
	} else {
		s.indices = s.indices[:size]
	}

	offset := 0
	for i, weight := range weights {
		for j := uint64(0); j < weight; j++ {
			s.indices[offset] = i
			offset++
		}
	}

	return nil
}

func (s *weightedUniform) Sample(value uint64) (int, error) {
	if uint64(len(s.indices)) <= value {
		return 0, errOutOfRange
	}
	return s.indices[int(value)], nil
}

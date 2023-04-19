// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
)

var (
	_ weightedWithoutReplacementSampler = (*weightedSampler)(nil)

	errOutOfRange     = errors.New("out of range")
	errWeightTooLarge = errors.New("total weight > math.MaxInt64")
	errNoWeights      = errors.New("weights sum to 0")
	errZeroWeight     = errors.New("weight can't be 0")
)

type weightedWithoutReplacementSampler interface {
	// Initialize sets the weights to sample with.
	// 0 will be sampled with probability weights[0] / sum(weights),
	// 1 will be sampled with probability weights[1] / sum(weights), etc.
	// initialize is O(sum(weights)).
	// Errors if sum(weights) > math.MaxInt64 or sum(weights) == 0.
	initialize(weights []uint64) error
	// Sample [n] elements from the sampler.
	// This sampler is without replacement, so the returned elements are unique.
	// The history of drawn elements is cleared after each call to sample,
	// so each call to sample is independent.
	// Returns errOutOfRange iff n > len(weights).
	sample(n int) ([]int, error)
}

// weightedSampler is a weighted sampler without replacement
type weightedSampler struct {
	weights           []uint64
	cumulativeWeights []uint64
	totalWeight       uint64
}

func (s *weightedSampler) initialize(weights []uint64) error {
	if len(weights) == 0 {
		return errNoWeights
	}

	totalWeight := uint64(0)
	cumulativeWeights := make([]uint64, len(weights))
	for i, weight := range weights {
		if weight == 0 {
			return errZeroWeight
		}
		if math.MaxInt64-totalWeight < weight {
			return errWeightTooLarge
		}
		totalWeight += weight
		cumulativeWeights[i] = totalWeight
	}
	// Don't set until we know the weights are valid.
	s.totalWeight = totalWeight
	s.cumulativeWeights = cumulativeWeights
	s.weights = weights
	return nil
}

func (s *weightedSampler) sample(n int) ([]int, error) {
	if len(s.cumulativeWeights) < n {
		return nil, fmt.Errorf("%w: %d < %d", errOutOfRange, len(s.cumulativeWeights), n)
	}

	// These are copied so they can be modified without affecting the sampler.
	currentTotalWeight := s.totalWeight
	cumulativeWeights := make([]uint64, len(s.cumulativeWeights))
	copy(cumulativeWeights, s.cumulativeWeights)

	result := make([]int, n)
	for i := 0; i < n; i++ {
		weight := rand.Int63n(int64(currentTotalWeight)) // #nosec G404
		drawn := findIndex(uint64(weight), cumulativeWeights)
		result[i] = drawn
		drawnWeight := s.weights[drawn]

		// Update the cumulative weights to reflect that we drew [drawn].
		currentTotalWeight -= drawnWeight
		for j := drawn; j < len(cumulativeWeights); j++ {
			cumulativeWeights[j] -= drawnWeight
		}
	}
	return result, nil
}

// Returns the index of the first instance of the smallest value
// in [cumulativeWeights] that is greater than [weight].
// Assumes that [cumulativeWeights] is sorted in ascending order.
// Assumes [weight] < cumulativeWeights[len(cumulativeWeights)-1].
func findIndex(weight uint64, cumulativeWeights []uint64) int {
	low := 0                           // Lowest possible candidate index.
	high := len(cumulativeWeights) - 1 // Highest possible candidate index.
	for {
		index := (low + high) / 2
		if cumulativeWeights[index] <= weight {
			low = index + 1
			continue
		}

		if index == 0 || cumulativeWeights[index-1] <= weight {
			return index
		}
		high = index - 1
	}
}

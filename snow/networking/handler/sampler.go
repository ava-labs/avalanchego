// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"errors"
	"fmt"
	"math"
	"math/rand"

	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ weightedWithoutReplacementSampler = (*weightedSampler)(nil)

	errOutOfRange     = errors.New("out of range")
	errWeightTooLarge = errors.New("total weight > math.MaxInt64")
	errNoWeights      = errors.New("weights sum to 0")
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
	cumulativeWeights []uint64
	drawn             set.Set[int]
	totalWeight       uint64
}

func (s *weightedSampler) initialize(weights []uint64) error {
	totalWeight := uint64(0)
	cumulativeWeights := make([]uint64, len(weights))
	for i, weight := range weights {
		if math.MaxInt64-totalWeight < weight {
			return errWeightTooLarge
		}
		totalWeight += weight
		cumulativeWeights[i] = totalWeight
	}
	if totalWeight == 0 {
		return errNoWeights
	}
	s.totalWeight = totalWeight
	s.cumulativeWeights = cumulativeWeights
	return nil
}

func (s *weightedSampler) sample(n int) ([]int, error) {
	if len(s.cumulativeWeights) < n {
		return nil, fmt.Errorf("%w: %d < %d", errOutOfRange, len(s.cumulativeWeights), n)
	}

	result := make([]int, n)
	for i := 0; i < n; i++ {
		for {
			weight := rand.Int63n(int64(s.totalWeight)) // #nosec G404
			drawn := findIndex(uint64(weight), s.cumulativeWeights)
			if !s.drawn.Contains(drawn) {
				result[i] = drawn
				s.drawn.Add(drawn)
				break
			}
			// We already drew [drawn] -- try again until we
			// draw something we haven't drawn yet.
		}
	}
	s.drawn.Clear()
	return result, nil
}

// Returns the index of the smallest value in [cumulativeWeights]
// that is greater than or equal to [weight].
// Assumes that [cumulativeWeights] is sorted in ascending order.
// Assumes [weight] <= cumulativeWeights[len(cumulativeWeights)-1].
func findIndex(weight uint64, cumulativeWeights []uint64) int {
	low := 0                           // Lowest possible candidate index.
	high := len(cumulativeWeights) - 1 // Highest possible candidate index.
	for {
		index := (low + high) / 2
		if weight > cumulativeWeights[index] {
			// The index we're looking for must be greater than [index].
			low = index + 1
			continue
		}

		if index == 0 || weight > cumulativeWeights[index-1] {
			// Either there is no index before [index], so this is the smallest one
			// meeting the condition, or the value at the previous index is less than [weight].
			return index
		}
		// The index we're looking for must be less than [index].
		high = index - 1
	}
}

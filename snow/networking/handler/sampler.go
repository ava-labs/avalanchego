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
	weights     []uint64
	elts        []int
	drawn       set.Set[int]
	totalWeight uint64
}

func (s *weightedSampler) initialize(weights []uint64) error {
	totalWeight := uint64(0)
	for _, weight := range weights {
		if math.MaxInt64-totalWeight < weight {
			return errWeightTooLarge
		}
		totalWeight += weight
	}
	if totalWeight == 0 {
		return errNoWeights
	}
	s.totalWeight = totalWeight
	s.weights = weights

	s.elts = make([]int, totalWeight)
	offset := 0
	for i, weight := range weights {
		for j := uint64(0); j < weight; j++ {
			s.elts[offset] = i
			offset++
		}
	}
	return nil
}

func (s *weightedSampler) sample(n int) ([]int, error) {
	if len(s.weights) < n {
		return nil, fmt.Errorf("%w: %d < %d", errOutOfRange, len(s.weights), n)
	}

	result := make([]int, n)
	for i := 0; i < n; i++ {
		for {
			index := rand.Int63n(int64(len(s.elts)))
			drawn := s.elts[index]
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

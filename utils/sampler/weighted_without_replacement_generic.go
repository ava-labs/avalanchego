// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	safemath "github.com/ava-labs/avalanchego/utils/math"
)

type weightedWithoutReplacementGeneric struct {
	u Uniform
	w Weighted
}

func (s *weightedWithoutReplacementGeneric) Initialize(weights []uint64) error {
	totalWeight := uint64(0)
	for _, weight := range weights {
		newWeight, err := safemath.Add(totalWeight, weight)
		if err != nil {
			return err
		}
		totalWeight = newWeight
	}
	s.u.Initialize(totalWeight)
	return s.w.Initialize(weights)
}

func (s *weightedWithoutReplacementGeneric) Sample(count int) ([]int, bool) {
	s.u.Reset()

	indices := make([]int, count)
	for i := 0; i < count; i++ {
		weight, ok := s.u.Next()
		if !ok {
			return nil, false
		}

		indices[i], ok = s.w.Sample(weight)
		if !ok {
			return nil, false
		}
	}
	return indices, true
}

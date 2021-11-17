// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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
		newWeight, err := safemath.Add64(totalWeight, weight)
		if err != nil {
			return err
		}
		totalWeight = newWeight
	}
	if err := s.u.Initialize(totalWeight); err != nil {
		return err
	}
	return s.w.Initialize(weights)
}

func (s *weightedWithoutReplacementGeneric) Sample(count int) ([]int, error) {
	s.u.Reset()

	indices := make([]int, count)
	for i := 0; i < count; i++ {
		weight, err := s.u.Next()
		if err != nil {
			return nil, err
		}
		indices[i], err = s.w.Sample(weight)
		if err != nil {
			return nil, err
		}
	}
	return indices, nil
}

func (s *weightedWithoutReplacementGeneric) Seed(seed int64) {
	s.u.Seed(seed)
}

func (s *weightedWithoutReplacementGeneric) ClearSeed() {
	s.u.ClearSeed()
}

// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	weights, err := s.u.Sample(count)
	if err != nil {
		return nil, err
	}
	indices := make([]int, count)
	for i, weight := range weights {
		indices[i], err = s.w.Sample(weight)
		if err != nil {
			return nil, err
		}
	}
	return indices, nil
}

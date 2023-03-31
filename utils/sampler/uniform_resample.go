// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"

	"github.com/ava-labs/avalanchego/utils/set"
)

// uniformResample allows for sampling over a uniform distribution without
// replacement.
//
// Sampling is performed by sampling with replacement and resampling if a
// duplicate is sampled.
//
// Initialization takes O(1) time.
//
// Sampling is performed in O(count) time and O(count) space.
type uniformResample struct {
	rng       rng
	seededRNG rng
	length    uint64
	drawn     set.Set[uint64]
}

func (s *uniformResample) Initialize(length uint64) error {
	if length > math.MaxInt64 {
		return errOutOfRange
	}
	s.rng = globalRNG
	s.seededRNG = newRNG()
	s.length = length
	s.drawn.Clear()
	return nil
}

func (s *uniformResample) Sample(count int) ([]uint64, error) {
	s.Reset()

	results := make([]uint64, count)
	for i := 0; i < count; i++ {
		ret, err := s.Next()
		if err != nil {
			return nil, err
		}
		results[i] = ret
	}
	return results, nil
}

func (s *uniformResample) Seed(seed int64) {
	s.rng = s.seededRNG
	s.rng.Seed(seed)
}

func (s *uniformResample) ClearSeed() {
	s.rng = globalRNG
}

func (s *uniformResample) Reset() {
	s.drawn.Clear()
}

func (s *uniformResample) Next() (uint64, error) {
	i := uint64(len(s.drawn))
	if i >= s.length {
		return 0, errOutOfRange
	}

	for {
		draw := uint64(s.rng.Int63n(int64(s.length)))
		if s.drawn.Contains(draw) {
			continue
		}
		s.drawn.Add(draw)
		return draw, nil
	}
}

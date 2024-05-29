// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

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
	rng    *rng
	length uint64
	drawn  map[uint64]struct{}
}

func (s *uniformResample) Initialize(length uint64) {
	s.length = length
	s.drawn = make(map[uint64]struct{})
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

func (s *uniformResample) Reset() {
	clear(s.drawn)
}

func (s *uniformResample) Next() (uint64, error) {
	i := uint64(len(s.drawn))
	if i >= s.length {
		return 0, ErrOutOfRange
	}

	for {
		draw := s.rng.Uint64Inclusive(s.length - 1)
		if _, ok := s.drawn[draw]; ok {
			continue
		}
		s.drawn[draw] = struct{}{}
		return draw, nil
	}
}

// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"math/rand"
	"time"
)

func init() { rand.Seed(time.Now().UnixNano()) }

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
	length uint64
	drawn  map[uint64]struct{}
}

func (s *uniformResample) Initialize(length uint64) error {
	if length > math.MaxInt64 {
		return errOutOfRange
	}
	s.length = length
	return nil
}

func (s *uniformResample) Sample(count int) ([]uint64, error) {
	if count < 0 || s.length < uint64(count) {
		return nil, errOutOfRange
	}

	if s.drawn == nil {
		s.drawn = make(map[uint64]struct{}, count)
	} else {
		for k := range s.drawn {
			delete(s.drawn, k)
		}
	}

	results := make([]uint64, count)
	for i := 0; i < count; {
		// We don't use a cryptographically secure source of randomness here, as
		// there's no need to ensure a truly random sampling.
		draw := uint64(rand.Int63n(int64(s.length))) // #nosec G404
		if _, ok := s.drawn[draw]; ok {
			continue
		}
		s.drawn[draw] = struct{}{}

		results[i] = draw
		i++
	}

	return results, nil
}

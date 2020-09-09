// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"errors"
	"math"
	"math/rand"
	"time"

	"github.com/ava-labs/avalanche-go/utils/timer"

	safemath "github.com/ava-labs/avalanche-go/utils/math"
)

var (
	errNoValidWeightedSamplers = errors.New("no valid weighted samplers found")
)

func init() { rand.Seed(time.Now().UnixNano()) }

// weightedBest implements the Weighted interface.
//
// Sampling is performed by using another implementation of the Weighted
// interface.
//
// Initialization attempts to find the best sampling algorithm given the dataset
// by performing a benchmark of the provided implementations.
type weightedBest struct {
	Weighted
	samplers            []Weighted
	benchmarkIterations int
	clock               timer.Clock
}

func (s *weightedBest) Initialize(weights []uint64) error {
	totalWeight := uint64(0)
	for _, weight := range weights {
		newWeight, err := safemath.Add64(totalWeight, weight)
		if err != nil {
			return err
		}
		totalWeight = newWeight
	}

	if totalWeight > math.MaxInt64 {
		return errWeightsTooLarge
	}

	samples := []uint64(nil)
	if totalWeight > 0 {
		samples = make([]uint64, s.benchmarkIterations)
		for i := range samples {
			// We don't need cryptographically secure random number generation
			// here, as the generated numbers are only used to perform an
			// optimistic benchmark. Which means the results could be arbitrary
			// and the correctness of the implementation wouldn't be effected.
			samples[i] = uint64(rand.Int63n(int64(totalWeight)))
		}
	}

	s.Weighted = nil
	bestDuration := time.Duration(math.MaxInt64)

samplerLoop:
	for _, sampler := range s.samplers {
		if err := sampler.Initialize(weights); err != nil {
			continue
		}

		start := s.clock.Time()
		for _, sample := range samples {
			if _, err := sampler.Sample(sample); err != nil {
				continue samplerLoop
			}
		}
		end := s.clock.Time()
		duration := end.Sub(start)
		if duration < bestDuration {
			bestDuration = duration
			s.Weighted = sampler
		}
	}

	if s.Weighted == nil {
		return errNoValidWeightedSamplers
	}
	return nil
}

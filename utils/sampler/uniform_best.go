// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ Uniform = (*uniformBest)(nil)

// Sampling is performed by using another implementation of the Uniform
// interface.
//
// Initialization attempts to find the best sampling algorithm given the dataset
// by performing a benchmark of the provided implementations.
type uniformBest struct {
	Uniform
	samplers            []Uniform
	maxSampleSize       int
	benchmarkIterations int
	clock               mockable.Clock
}

// NewBestUniform returns a new sampler
func NewBestUniform(expectedSampleSize int) Uniform {
	return &uniformBest{
		samplers: []Uniform{
			&uniformReplacer{
				rng: globalRNG,
			},
			&uniformResample{
				rng: globalRNG,
			},
		},
		maxSampleSize:       expectedSampleSize,
		benchmarkIterations: 100,
	}
}

func (s *uniformBest) Initialize(length uint64) {
	s.Uniform = nil
	bestDuration := time.Duration(math.MaxInt64)

	sampleSize := s.maxSampleSize
	if length < uint64(sampleSize) {
		sampleSize = int(length)
	}

samplerLoop:
	for _, sampler := range s.samplers {
		sampler.Initialize(length)

		start := s.clock.Time()
		for i := 0; i < s.benchmarkIterations; i++ {
			if _, ok := sampler.Sample(sampleSize); !ok {
				continue samplerLoop
			}
		}
		end := s.clock.Time()
		duration := end.Sub(start)
		if duration < bestDuration {
			bestDuration = duration
			s.Uniform = sampler
		}
	}

	s.Uniform.Reset()
}

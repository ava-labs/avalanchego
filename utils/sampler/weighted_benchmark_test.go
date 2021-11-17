// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"errors"
	"fmt"
	"math"
	"testing"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// BenchmarkAllWeightedSampling
func BenchmarkAllWeightedSampling(b *testing.B) {
	pows := []float64{
		0,
		1,
		2,
		3,
	}
	sizes := []int{
		10,
		500,
		1000,
		50000,
		100000,
	}
	for _, s := range weightedSamplers {
		for _, pow := range pows {
			for _, size := range sizes {
				if WeightedPowBenchmarkSampler(b, s.sampler, pow, size) {
					b.Run(fmt.Sprintf("sampler %s with %d elements at x^%.1f", s.name, size, pow), func(b *testing.B) {
						WeightedPowBenchmarkSampler(b, s.sampler, pow, size)
					})
				}
			}
		}
		for _, size := range sizes {
			if WeightedSingletonBenchmarkSampler(b, s.sampler, size) {
				b.Run(fmt.Sprintf("sampler %s with %d singleton elements", s.name, size), func(b *testing.B) {
					WeightedSingletonBenchmarkSampler(b, s.sampler, size)
				})
			}
		}
	}
}

// BenchmarkAllWeightedInitializer
func BenchmarkAllWeightedInitializer(b *testing.B) {
	pows := []float64{
		0,
		1,
		2,
		3,
	}
	sizes := []int{
		10,
		500,
		1000,
		50000,
		100000,
	}
	for _, s := range weightedSamplers {
		for _, pow := range pows {
			for _, size := range sizes {
				if WeightedPowBenchmarkSampler(b, s.sampler, pow, size) {
					b.Run(fmt.Sprintf("initializer %s with %d elements at x^%.1f", s.name, size, pow), func(b *testing.B) {
						WeightedPowBenchmarkInitializer(b, s.sampler, pow, size)
					})
				}
			}
		}
		for _, size := range sizes {
			if WeightedSingletonBenchmarkSampler(b, s.sampler, size) {
				b.Run(fmt.Sprintf("initializer %s with %d singleton elements", s.name, size), func(b *testing.B) {
					WeightedSingletonBenchmarkInitializer(b, s.sampler, size)
				})
			}
		}
	}
}

func CalcWeightedPoW(exponent float64, size int) (uint64, []uint64, error) {
	weights := make([]uint64, size)
	totalWeight := uint64(0)
	for i := range weights {
		weight := uint64(math.Pow(float64(i+1), exponent))
		weights[i] = weight

		newWeight, err := safemath.Add64(totalWeight, weight)
		if err != nil {
			return 0, nil, err
		}
		totalWeight = newWeight
	}
	if totalWeight > math.MaxInt64 {
		return 0, nil, errors.New("overflow error")
	}
	return totalWeight, weights, nil
}

func WeightedPowBenchmarkSampler(
	b *testing.B,
	s Weighted,
	exponent float64,
	size int,
) bool {
	totalWeight, weights, err := CalcWeightedPoW(exponent, size)
	if err != nil {
		return false
	}
	if err := s.Initialize(weights); err != nil {
		return false
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Sample(uint64(globalRNG.Int63n(int64(totalWeight))))
	}
	return true
}

func WeightedSingletonBenchmarkSampler(b *testing.B, s Weighted, size int) bool {
	weights := make([]uint64, size)
	weights[0] = uint64(math.MaxInt64 - size + 1)
	for i := 1; i < len(weights); i++ {
		weights[i] = 1
	}

	err := s.Initialize(weights)
	if err != nil {
		return false
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Sample(uint64(globalRNG.Int63n(math.MaxInt64)))
	}
	return true
}

func WeightedPowBenchmarkInitializer(
	b *testing.B,
	s Weighted,
	exponent float64,
	size int,
) {
	_, weights, _ := CalcWeightedPoW(exponent, size)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Initialize(weights)
	}
}

func WeightedSingletonBenchmarkInitializer(b *testing.B, s Weighted, size int) {
	weights := make([]uint64, size)
	weights[0] = uint64(math.MaxInt64 - size + 1)
	for i := 1; i < len(weights); i++ {
		weights[i] = 1
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Initialize(weights)
	}
}

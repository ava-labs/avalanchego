// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"math"
	"testing"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func BenchmarkWeightedHeapSampling(b *testing.B) {
	sampler := &weightedHeap{}
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
	for _, pow := range pows {
		for _, size := range sizes {
			if WeightedPowBenchmarkSampler(b, sampler, pow, size) {
				b.Run(fmt.Sprintf("%d elements at x^%.1f", size, pow), func(b *testing.B) {
					WeightedPowBenchmarkSampler(b, sampler, pow, size)
				})
			}
		}
	}
	for _, size := range sizes {
		if WeightedSingletonBenchmarkSampler(b, sampler, size) {
			b.Run(fmt.Sprintf(" %d singleton elements", size), func(b *testing.B) {
				WeightedSingletonBenchmarkSampler(b, sampler, size)
			})
		}
	}
}

func BenchmarkWeightedHeapInitializer(b *testing.B) {
	sampler := &weightedHeap{}
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
	for _, pow := range pows {
		for _, size := range sizes {
			if WeightedPowBenchmarkSampler(b, sampler, pow, size) {
				b.Run(fmt.Sprintf("%d elements at x^%.1f", size, pow), func(b *testing.B) {
					_, weights, _ := CalcWeightedPoW(pow, size)

					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						_ = sampler.Initialize(weights)
					}
				})
			}
		}
	}
	for _, size := range sizes {
		if WeightedSingletonBenchmarkSampler(b, sampler, size) {
			b.Run(fmt.Sprintf("%d singleton elements", size), func(b *testing.B) {
				weights := make([]uint64, size)
				weights[0] = math.MaxUint64 - uint64(size-1)
				for i := 1; i < len(weights); i++ {
					weights[i] = 1
				}

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = sampler.Initialize(weights)
				}
			})
		}
	}
}

func CalcWeightedPoW(exponent float64, size int) (uint64, []uint64, error) {
	weights := make([]uint64, size)
	totalWeight := uint64(0)
	for i := range weights {
		weight := uint64(math.Pow(float64(i+1), exponent))
		weights[i] = weight

		newWeight, err := safemath.Add(totalWeight, weight)
		if err != nil {
			return 0, nil, err
		}
		totalWeight = newWeight
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
		_, _ = s.Sample(globalRNG.Uint64Inclusive(totalWeight - 1))
	}
	return true
}

func WeightedSingletonBenchmarkSampler(b *testing.B, s Weighted, size int) bool {
	weights := make([]uint64, size)
	weights[0] = math.MaxUint64 - uint64(size-1)
	for i := 1; i < len(weights); i++ {
		weights[i] = 1
	}

	err := s.Initialize(weights)
	if err != nil {
		return false
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Sample(globalRNG.Uint64Inclusive(math.MaxUint64 - 1))
	}
	return true
}

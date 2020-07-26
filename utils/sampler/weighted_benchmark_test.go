// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"testing"

	safemath "github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/utils/random"
)

func WeightedUniformBenchmark(b *testing.B, s Weighted, size int) {
	weights := make([]uint64, size)
	for i := range weights {
		weights[i] = 1
	}
	s.Initialize(weights)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		s.StartSearch(uint64(random.Rand(0, len(weights))))
		for {
			if _, ok := s.ContinueSearch(); ok {
				break
			}
		}
	}
}

func WeightedPowBenchmark(b *testing.B, s Weighted, exponent float64, size int) {
	weights := make([]uint64, size)
	maxWeight := uint64(0)
	for i := range weights {
		weight := uint64(math.Pow(float64(i+1), exponent))
		weights[i] = weight

		newWeight, err := safemath.Add64(maxWeight, weight)
		if err != nil {
			b.Fatal(err)
		}
		maxWeight = newWeight
	}
	s.Initialize(weights)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		s.StartSearch(uint64(random.Rand(0, int(maxWeight))))
		for {
			if _, ok := s.ContinueSearch(); ok {
				break
			}
		}
	}
}

func WeightedExponentialBenchmark(b *testing.B, s Weighted, size int) {
	if size > 60 {
		b.Fatalf("This test can only be performed with a small number of values")
	}

	weights := make([]uint64, size)
	currentWeight := uint64(math.MaxUint64 / 4)
	maxWeight := uint64(0)
	for i := range weights {
		weights[i] = currentWeight
		maxWeight += currentWeight
		currentWeight /= 2
	}
	s.Initialize(weights)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		s.StartSearch(uint64(random.Rand(0, int(maxWeight))))
		for {
			if _, ok := s.ContinueSearch(); ok {
				break
			}
		}
	}
}

// BenchmarkRNG
func BenchmarkRNG(b *testing.B) {
	for n := 0; n < b.N; n++ {
		_ = random.Rand(0, 10)
	}
}

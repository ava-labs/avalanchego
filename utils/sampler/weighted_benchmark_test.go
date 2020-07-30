// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"testing"

	"github.com/ava-labs/gecko/utils/random"

	safemath "github.com/ava-labs/gecko/utils/math"
)

func WeightedUniformBenchmark(b *testing.B, s Weighted, size int) {
	weights := make([]uint64, size)
	for i := range weights {
		weights[i] = 1
	}
	s.Initialize(weights)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Sample(uint64(random.Rand(0, len(weights))))
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
	if maxWeight > math.MaxInt64 {
		b.Fatalf("overflow error")
	}

	s.Initialize(weights)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Sample(uint64(random.Rand(0, int(maxWeight))))
	}
}

func WeightedSingletonBenchmark(b *testing.B, s Weighted, size int) {
	weights := make([]uint64, size)
	weights[0] = uint64(math.MaxInt64 - size + 1)
	for i := 1; i < len(weights); i++ {
		weights[i] = 1
	}
	s.Initialize(weights)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Sample(uint64(random.Rand(0, math.MaxInt64)))
	}
}

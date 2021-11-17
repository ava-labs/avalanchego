// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"testing"
)

// BenchmarkAllWeightedWithoutReplacement
func BenchmarkAllWeightedWithoutReplacement(b *testing.B) {
	sizes := []int{
		1,
		5,
		25,
		50,
		75,
		100,
	}
	for _, s := range weightedWithoutReplacementSamplers {
		for _, size := range sizes {
			b.Run(fmt.Sprintf("sampler %s with %d elements", s.name, size), func(b *testing.B) {
				WeightedWithoutReplacementPowBenchmark(
					b,
					s.sampler,
					0,
					100000,
					size,
				)
			})
		}
	}
}

func WeightedWithoutReplacementPowBenchmark(
	b *testing.B,
	s WeightedWithoutReplacement,
	exponent float64,
	size int,
	count int,
) {
	_, weights, err := CalcWeightedPoW(exponent, size)
	if err != nil {
		b.Fatal(err)
	}
	if err := s.Initialize(weights); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Sample(count)
	}
}

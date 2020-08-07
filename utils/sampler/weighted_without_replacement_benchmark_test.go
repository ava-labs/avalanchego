// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"
)

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

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"testing"
)

// BenchmarkAllUniform
func BenchmarkAllUniform(b *testing.B) {
	sizes := []uint64{
		30,
		35,
		500,
		10000,
		100000,
	}
	for _, s := range uniformSamplers {
		for _, size := range sizes {
			b.Run(fmt.Sprintf("sampler %s with %d elements uniformly", s.name, size), func(b *testing.B) {
				UniformBenchmark(b, s.sampler, size, 30)
			})
		}
	}
}

func UniformBenchmark(b *testing.B, s Uniform, size uint64, toSample int) {
	err := s.Initialize(size)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Sample(toSample)
	}
}

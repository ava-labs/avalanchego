// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"testing"
)

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

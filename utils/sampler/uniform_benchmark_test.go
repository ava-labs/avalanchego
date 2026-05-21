// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"testing"
)

func BenchmarkUniform(b *testing.B) {
	sizes := []uint64{
		30,
		35,
		500,
		10000,
		100000,
	}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%d elements uniformly", size), func(b *testing.B) {
			s := NewUniform()

			s.Initialize(size)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = s.Sample(30)
			}
		})
	}
}

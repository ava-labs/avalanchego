// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkWeightedWithoutReplacement(b *testing.B) {
	sizes := []int{
		1,
		5,
		25,
		50,
		75,
		100,
	}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%d elements", size), func(b *testing.B) {
			require := require.New(b)
			s := NewWeightedWithoutReplacement()

			_, weights, err := CalcWeightedPoW(0, 100000)
			require.NoError(err)
			require.NoError(s.Initialize(weights))

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = s.Sample(size)
			}
		})
	}
}

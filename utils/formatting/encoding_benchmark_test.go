// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/units"
)

func BenchmarkEncodings(b *testing.B) {
	benchmarks := []struct {
		encoding Encoding
		size     int
	}{
		{
			encoding: Hex,
			size:     1 * units.KiB, // 1kb
		},
		{
			encoding: Hex,
			size:     4 * units.KiB, // 4kb
		},
		{
			encoding: Hex,
			size:     32 * units.KiB, // 32kb
		},
		{
			encoding: Hex,
			size:     128 * units.KiB, // 128kb
		},
		{
			encoding: Hex,
			size:     256 * units.KiB, // 256kb
		},
		{
			encoding: Hex,
			size:     512 * units.KiB, // 512kb
		},
		{
			encoding: Hex,
			size:     1 * units.MiB, // 1mb
		},
		{
			encoding: Hex,
			size:     2 * units.MiB, // 2mb
		},
		{
			encoding: Hex,
			size:     4 * units.MiB, // 4mb
		},
	}
	for _, benchmark := range benchmarks {
		bytes := make([]byte, benchmark.size)
		_, _ = rand.Read(bytes) // #nosec G404
		b.Run(fmt.Sprintf("%s-%d bytes", benchmark.encoding, benchmark.size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_, err := Encode(benchmark.encoding, bytes)
				require.NoError(b, err)
			}
		})
	}
}

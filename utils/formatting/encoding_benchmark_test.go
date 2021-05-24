// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"fmt"
	"math/rand"
	"testing"
)

func BenchmarkEncodings(b *testing.B) {
	benchmarks := []struct {
		encoding Encoding
		size     int
	}{
		{
			encoding: CB58,
			size:     1 << 10, // 1kb
		},
		{
			encoding: CB58,
			size:     1 << 11, // 2kb
		},
		{
			encoding: CB58,
			size:     1 << 12, // 4kb
		},
		{
			encoding: CB58,
			size:     1 << 13, // 8kb
		},
		{
			encoding: CB58,
			size:     1 << 14, // 16kb
		},
		{
			encoding: CB58,
			size:     1 << 15, // 32kb
		},
		{
			encoding: Hex,
			size:     1 << 10, // 1kb
		},
		{
			encoding: Hex,
			size:     1 << 12, // 4kb
		},
		{
			encoding: Hex,
			size:     1 << 15, // 32kb
		},
		{
			encoding: Hex,
			size:     1 << 17, // 128kb
		},
		{
			encoding: Hex,
			size:     1 << 18, // 256kb
		},
		{
			encoding: Hex,
			size:     1 << 19, // 512kb
		},
		{
			encoding: Hex,
			size:     1 << 20, // 1mb
		},
		{
			encoding: Hex,
			size:     1 << 21, // 2mb
		},
		{
			encoding: Hex,
			size:     1 << 22, // 4mb
		},
	}
	for _, benchmark := range benchmarks {
		bytes := make([]byte, benchmark.size)
		_, _ = rand.Read(bytes) // #nosec G404
		b.Run(fmt.Sprintf("%s-%d bytes", benchmark.encoding, benchmark.size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				if _, err := Encode(benchmark.encoding, bytes); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

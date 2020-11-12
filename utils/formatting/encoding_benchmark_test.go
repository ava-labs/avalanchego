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
		encoding string
		size     int
	}{
		{
			encoding: CB58Encoding,
			size:     1 << 10, // 1kb
		},
		{
			encoding: CB58Encoding,
			size:     1 << 11, // 2kb
		},
		{
			encoding: CB58Encoding,
			size:     1 << 12, // 4kb
		},
		{
			encoding: CB58Encoding,
			size:     1 << 13, // 8kb
		},
		{
			encoding: CB58Encoding,
			size:     1 << 14, // 16kb
		},
		{
			encoding: CB58Encoding,
			size:     1 << 15, // 32kb
		},
		{
			encoding: HexEncoding,
			size:     1 << 10, // 1kb
		},
		{
			encoding: HexEncoding,
			size:     1 << 12, // 4kb
		},
		{
			encoding: HexEncoding,
			size:     1 << 15, // 32kb
		},
		{
			encoding: HexEncoding,
			size:     1 << 17, // 128kb
		},
		{
			encoding: HexEncoding,
			size:     1 << 18, // 256kb
		},
		{
			encoding: HexEncoding,
			size:     1 << 19, // 512kb
		},
		{
			encoding: HexEncoding,
			size:     1 << 20, // 1mb
		},
		{
			encoding: HexEncoding,
			size:     1 << 21, // 2mb
		},
		{
			encoding: HexEncoding,
			size:     1 << 22, // 4mb
		},
	}
	manager, _ := NewEncodingManager(HexEncoding)
	for _, benchmark := range benchmarks {
		enc, _ := manager.GetEncoding(benchmark.encoding)
		bytes := make([]byte, benchmark.size)
		_, _ = rand.Read(bytes) // #nosec G404
		b.Run(fmt.Sprintf("%s-%d bytes", benchmark.encoding, benchmark.size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_ = enc.ConvertBytes(bytes)
			}
		})
	}
}

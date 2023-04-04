// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
)

const maxMessageSize = 2 * units.MiB // Max message size. Can't import due to cycle.

func TestCompressDecompress(t *testing.T) {
	for _, compressionType := range []Type{TypeNone, TypeGzip, TypeZstd} {
		t.Run(compressionType.String(), func(t *testing.T) {
			data := make([]byte, 4096)
			for i := 0; i < len(data); i++ {
				data[i] = byte(rand.Intn(256)) // #nosec G404
			}

			data2 := make([]byte, 4096)
			for i := 0; i < len(data); i++ {
				data2[i] = byte(rand.Intn(256)) // #nosec G404
			}

			var (
				compressor Compressor
				err        error
			)
			switch compressionType {
			case TypeNone:
				compressor = &noCompressor{}
			case TypeGzip:
				var err error
				compressor, err = NewGzipCompressor(maxMessageSize)
				require.NoError(t, err)
			case TypeZstd:
				compressor, err = NewZstdCompressor(maxMessageSize)
				require.NoError(t, err)
			default:
				require.FailNow(t, "Unknown compression type")
			}

			dataCompressed, err := compressor.Compress(data)
			require.NoError(t, err)

			data2Compressed, err := compressor.Compress(data2)
			require.NoError(t, err)

			dataDecompressed, err := compressor.Decompress(dataCompressed)
			require.NoError(t, err)
			require.EqualValues(t, data, dataDecompressed)

			data2Decompressed, err := compressor.Decompress(data2Compressed)
			require.NoError(t, err)
			require.EqualValues(t, data2, data2Decompressed)

			dataDecompressed, err = compressor.Decompress(dataCompressed)
			require.NoError(t, err)
			require.EqualValues(t, data, dataDecompressed)

			maxMessage := make([]byte, 2*units.MiB) // Max message size. Can't import due to cycle.
			_, err = rand.Read(maxMessage)          // #nosec G404
			require.NoError(t, err)

			maxMessageCompressed, err := compressor.Compress(maxMessage)
			require.NoError(t, err)

			maxMessageDecompressed, err := compressor.Decompress(maxMessageCompressed)
			require.NoError(t, err)

			require.EqualValues(t, maxMessage, maxMessageDecompressed)
		})
	}
}

func TestGzipSizeLimiting(t *testing.T) {
	compressorFuncs := map[string]func(int64) (Compressor, error){
		"gzip": NewGzipCompressor,
		"zstd": NewZstdCompressor,
	}

	for name, compressorFunc := range compressorFuncs {
		t.Run(name, func(t *testing.T) {
			compressor, err := compressorFunc(maxMessageSize)
			require.NoError(t, err)

			data := make([]byte, maxMessageSize+1)
			_, err = compressor.Compress(data) // should be too large
			require.Error(t, err)

			compressor2, err := compressorFunc(2 * maxMessageSize)
			require.NoError(t, err)

			dataCompressed, err := compressor2.Compress(data)
			require.NoError(t, err)

			_, err = compressor.Decompress(dataCompressed) // should be too large
			require.Error(t, err)
		})
	}
}

// Attempts to create gzip compressor with math.MaxInt64
// which leads to undefined decompress behavior due to integer overflow
// in limit reader creation.
func TestNewCompressorWithInvalidLimit(t *testing.T) {
	compressorFuncs := map[string]func(int64) (Compressor, error){
		"gzip": NewGzipCompressor,
		"zstd": NewZstdCompressor,
	}

	for name, compressorFunc := range compressorFuncs {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			_, err := compressorFunc(math.MaxInt64)
			require.ErrorIs(err, ErrInvalidMaxSizeCompressor)
		})
	}
}

func FuzzGzipCompressor(f *testing.F) {
	fuzzHelper(f, TypeGzip)
}

func FuzzZstdCompressor(f *testing.F) {
	fuzzHelper(f, TypeZstd)
}

func fuzzHelper(f *testing.F, compressionType Type) {
	var (
		compressor Compressor
		err        error
	)
	switch compressionType {
	case TypeGzip:
		compressor, err = NewGzipCompressor(maxMessageSize)
		require.NoError(f, err)
	case TypeZstd:
		compressor, err = NewZstdCompressor(maxMessageSize)
		require.NoError(f, err)
	default:
		f.Fatal("Unknown compression type")
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		if len(data) > 2*units.MiB {
			_, err := compressor.Compress(data)
			require.Error(err)
		}

		compressed, err := compressor.Compress(data)
		require.NoError(err)

		decompressed, err := compressor.Decompress(compressed)
		require.NoError(err)

		require.Equal(data, decompressed)
	})
}

func BenchmarkGzipCompress(b *testing.B) {
	sizes := []int{
		0,
		256,
		units.KiB,
		units.MiB,
	}
	for _, size := range sizes {
		bytes := utils.RandomBytes(size)
		compressor, err := NewGzipCompressor(2 * units.MiB)
		require.NoError(b, err)

		b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_, err := compressor.Compress(bytes)
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkGzipDecompress(b *testing.B) {
	sizes := []int{
		0,
		256,
		units.KiB,
		units.MiB,
	}
	for _, size := range sizes {
		bytes := utils.RandomBytes(size)
		compressor, err := NewGzipCompressor(2 * units.MiB)
		require.NoError(b, err)

		compressedBytes, err := compressor.Compress(bytes)
		require.NoError(b, err)

		b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_, err := compressor.Decompress(compressedBytes)
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkZstdCompress(b *testing.B) {
	sizes := []int{
		0,
		256,
		units.KiB,
		units.MiB,
	}
	for _, size := range sizes {
		bytes := utils.RandomBytes(size)
		compressor, err := NewZstdCompressor(2 * units.MiB)
		require.NoError(b, err)

		b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_, err := compressor.Compress(bytes)
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkZstdDecompress(b *testing.B) {
	sizes := []int{
		0,
		256,
		units.KiB,
		units.MiB,
	}
	for _, size := range sizes {
		bytes := utils.RandomBytes(size)
		compressor, err := NewZstdCompressor(2 * units.MiB)
		require.NoError(b, err)

		compressedBytes, err := compressor.Compress(bytes)
		require.NoError(b, err)

		b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_, err := compressor.Decompress(compressedBytes)
				require.NoError(b, err)
			}
		})
	}
}

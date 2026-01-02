// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/DataDog/zstd"
	"github.com/stretchr/testify/require"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
)

const maxMessageSize = 2 * units.MiB // Max message size. Can't import due to cycle.

var (
	newCompressorFuncs = map[Type]func(maxSize int64) (Compressor, error){
		TypeNone: func(int64) (Compressor, error) { //nolint:unparam // an error is needed to be returned to compile
			return NewNoCompressor(), nil
		},
		TypeZstd: NewZstdCompressor,
	}

	//go:embed zstd_zip_bomb.bin
	zstdZipBomb []byte

	zipBombs = map[Type][]byte{
		TypeZstd: zstdZipBomb,
	}
)

func TestDecompressZipBombs(t *testing.T) {
	for compressionType, zipBomb := range zipBombs {
		// Make sure that the hardcoded zip bomb would be a valid message.
		require.Less(t, len(zipBomb), maxMessageSize)

		newCompressorFunc := newCompressorFuncs[compressionType]

		t.Run(compressionType.String(), func(t *testing.T) {
			require := require.New(t)

			compressor, err := newCompressorFunc(maxMessageSize)
			require.NoError(err)

			var (
				beforeDecompressionStats runtime.MemStats
				afterDecompressionStats  runtime.MemStats
			)
			runtime.ReadMemStats(&beforeDecompressionStats)
			_, err = compressor.Decompress(zipBomb)
			runtime.ReadMemStats(&afterDecompressionStats)

			require.ErrorIs(err, ErrDecompressedMsgTooLarge)

			// Make sure that we didn't allocate significantly more memory than
			// the max message size.
			bytesAllocatedDuringDecompression := afterDecompressionStats.TotalAlloc - beforeDecompressionStats.TotalAlloc
			require.Less(bytesAllocatedDuringDecompression, uint64(10*maxMessageSize))
		})
	}
}

func TestCompressDecompress(t *testing.T) {
	for compressionType, newCompressorFunc := range newCompressorFuncs {
		t.Run(compressionType.String(), func(t *testing.T) {
			require := require.New(t)

			data := utils.RandomBytes(4096)
			data2 := utils.RandomBytes(4096)

			compressor, err := newCompressorFunc(maxMessageSize)
			require.NoError(err)

			dataCompressed, err := compressor.Compress(data)
			require.NoError(err)

			data2Compressed, err := compressor.Compress(data2)
			require.NoError(err)

			dataDecompressed, err := compressor.Decompress(dataCompressed)
			require.NoError(err)
			require.Equal(data, dataDecompressed)

			data2Decompressed, err := compressor.Decompress(data2Compressed)
			require.NoError(err)
			require.Equal(data2, data2Decompressed)

			dataDecompressed, err = compressor.Decompress(dataCompressed)
			require.NoError(err)
			require.Equal(data, dataDecompressed)

			maxMessage := utils.RandomBytes(maxMessageSize)
			maxMessageCompressed, err := compressor.Compress(maxMessage)
			require.NoError(err)

			maxMessageDecompressed, err := compressor.Decompress(maxMessageCompressed)
			require.NoError(err)

			require.Equal(maxMessage, maxMessageDecompressed)
		})
	}
}

func TestSizeLimiting(t *testing.T) {
	for compressionType, compressorFunc := range newCompressorFuncs {
		if compressionType == TypeNone {
			continue
		}
		t.Run(compressionType.String(), func(t *testing.T) {
			require := require.New(t)

			compressor, err := compressorFunc(maxMessageSize)
			require.NoError(err)

			data := make([]byte, maxMessageSize+1)
			_, err = compressor.Compress(data) // should be too large
			require.ErrorIs(err, ErrMsgTooLarge)

			compressor2, err := compressorFunc(2 * maxMessageSize)
			require.NoError(err)

			dataCompressed, err := compressor2.Compress(data)
			require.NoError(err)

			_, err = compressor.Decompress(dataCompressed) // should be too large
			require.ErrorIs(err, ErrDecompressedMsgTooLarge)
		})
	}
}

// Attempts to create a compressor with math.MaxInt64
// which leads to undefined decompress behavior due to integer overflow
// in limit reader creation.
func TestNewCompressorWithInvalidLimit(t *testing.T) {
	for compressionType, compressorFunc := range newCompressorFuncs {
		if compressionType == TypeNone {
			continue
		}
		t.Run(compressionType.String(), func(t *testing.T) {
			_, err := compressorFunc(math.MaxInt64)
			require.ErrorIs(t, err, ErrInvalidMaxSizeCompressor)
		})
	}
}

func TestNewZstdCompressorWithLevel(t *testing.T) {
	compressor, err := NewZstdCompressorWithLevel(maxMessageSize, zstd.BestSpeed)
	require.NoError(t, err)

	data := utils.RandomBytes(4096)
	compressed, err := compressor.Compress(data)
	require.NoError(t, err)

	decompressed, err := compressor.Decompress(compressed)
	require.NoError(t, err)
	require.Equal(t, data, decompressed)
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
	case TypeZstd:
		compressor, err = NewZstdCompressor(maxMessageSize)
		require.NoError(f, err)
	default:
		require.FailNow(f, "Unknown compression type")
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		if len(data) > maxMessageSize {
			_, err := compressor.Compress(data)
			require.ErrorIs(err, ErrMsgTooLarge)
		}

		compressed, err := compressor.Compress(data)
		require.NoError(err)

		decompressed, err := compressor.Decompress(compressed)
		require.NoError(err)

		require.Equal(data, decompressed)
	})
}

func BenchmarkCompress(b *testing.B) {
	sizes := []int{
		0,
		256,
		units.KiB,
		units.MiB,
		maxMessageSize,
	}
	for compressionType, newCompressorFunc := range newCompressorFuncs {
		if compressionType == TypeNone {
			continue
		}
		for _, size := range sizes {
			b.Run(fmt.Sprintf("%s_%d", compressionType, size), func(b *testing.B) {
				require := require.New(b)

				bytes := utils.RandomBytes(size)
				compressor, err := newCompressorFunc(maxMessageSize)
				require.NoError(err)
				for n := 0; n < b.N; n++ {
					_, err := compressor.Compress(bytes)
					require.NoError(err)
				}
			})
		}
	}
}

func BenchmarkDecompress(b *testing.B) {
	sizes := []int{
		0,
		256,
		units.KiB,
		units.MiB,
		maxMessageSize,
	}
	for compressionType, newCompressorFunc := range newCompressorFuncs {
		if compressionType == TypeNone {
			continue
		}
		for _, size := range sizes {
			b.Run(fmt.Sprintf("%s_%d", compressionType, size), func(b *testing.B) {
				require := require.New(b)

				bytes := utils.RandomBytes(size)
				compressor, err := newCompressorFunc(maxMessageSize)
				require.NoError(err)

				compressedBytes, err := compressor.Compress(bytes)
				require.NoError(err)

				for n := 0; n < b.N; n++ {
					_, err := compressor.Decompress(compressedBytes)
					require.NoError(err)
				}
			})
		}
	}
}

// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"bytes"
	"compress/gzip"
	"math"
	"runtime"
	"testing"

	"github.com/DataDog/zstd"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
)

const maxMessageSize = 2 * units.MiB // Max message size. Can't import due to cycle.

var newCompressorFuncs = map[Type]func(maxSize int64) (Compressor, error){
	TypeNone: func(int64) (Compressor, error) { //nolint:unparam // an error is needed to be returned to compile
		return NewNoCompressor(), nil
	},
	TypeGzip: NewGzipCompressor,
	TypeZstd: NewZstdCompressor,
}

func TestDecompressGzipBomb(t *testing.T) {
	var (
		buf               bytes.Buffer
		gzipWriter        = gzip.NewWriter(&buf)
		totalWrittenBytes uint64
		data              = make([]byte, units.MiB)
	)
	for buf.Len() < maxMessageSize {
		n, err := gzipWriter.Write(data)
		require.NoError(t, err)
		totalWrittenBytes += uint64(n)
	}
	require.NoError(t, gzipWriter.Close())

	compressor, err := NewGzipCompressor(maxMessageSize)
	require.NoError(t, err)

	compressedBytes := buf.Bytes()

	var (
		beforeDecompressionStats runtime.MemStats
		afterDecompressionStats  runtime.MemStats
	)

	runtime.ReadMemStats(&beforeDecompressionStats)
	_, err = compressor.Decompress(compressedBytes)
	runtime.ReadMemStats(&afterDecompressionStats)

	require.ErrorIs(t, err, ErrDecompressedMsgTooLarge)

	bytesAllocatedDuringDecompression := afterDecompressionStats.TotalAlloc - beforeDecompressionStats.TotalAlloc
	require.Less(t, bytesAllocatedDuringDecompression, totalWrittenBytes)
}

func TestDecompressZstdBomb(t *testing.T) {
	var (
		buf               bytes.Buffer
		zstdWriter        = zstd.NewWriter(&buf)
		totalWrittenBytes uint64
		data              = make([]byte, units.MiB)
	)
	for buf.Len() < maxMessageSize {
		n, err := zstdWriter.Write(data)
		require.NoError(t, err)
		totalWrittenBytes += uint64(n)
	}
	require.NoError(t, zstdWriter.Close())

	compressor, err := NewZstdCompressor(maxMessageSize)
	require.NoError(t, err)

	compressedBytes := buf.Bytes()

	var (
		beforeDecompressionStats runtime.MemStats
		afterDecompressionStats  runtime.MemStats
	)

	runtime.ReadMemStats(&beforeDecompressionStats)
	_, err = compressor.Decompress(compressedBytes)
	runtime.ReadMemStats(&afterDecompressionStats)

	require.ErrorIs(t, err, ErrDecompressedMsgTooLarge)

	bytesAllocatedDuringDecompression := afterDecompressionStats.TotalAlloc - beforeDecompressionStats.TotalAlloc
	require.Less(t, bytesAllocatedDuringDecompression, totalWrittenBytes)
}

func TestCompressDecompress(t *testing.T) {
	for compressionType, newCompressorFunc := range newCompressorFuncs {
		t.Run(compressionType.String(), func(t *testing.T) {
			data := utils.RandomBytes(4096)
			data2 := utils.RandomBytes(4096)

			compressor, err := newCompressorFunc(maxMessageSize)
			require.NoError(t, err)

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

			maxMessage := utils.RandomBytes(maxMessageSize)
			maxMessageCompressed, err := compressor.Compress(maxMessage)
			require.NoError(t, err)

			maxMessageDecompressed, err := compressor.Decompress(maxMessageCompressed)
			require.NoError(t, err)

			require.EqualValues(t, maxMessage, maxMessageDecompressed)
		})
	}
}

func TestSizeLimiting(t *testing.T) {
	for compressionType, compressorFunc := range newCompressorFuncs {
		if compressionType == TypeNone {
			continue
		}
		t.Run(compressionType.String(), func(t *testing.T) {
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

// Attempts to create a compressor with math.MaxInt64
// which leads to undefined decompress behavior due to integer overflow
// in limit reader creation.
func TestNewCompressorWithInvalidLimit(t *testing.T) {
	for compressionType, compressorFunc := range newCompressorFuncs {
		if compressionType == TypeNone {
			continue
		}
		t.Run(compressionType.String(), func(t *testing.T) {
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

		if len(data) > maxMessageSize {
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

// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/units"
)

const maxMessageSize = 2 * units.MiB // Max message size. Can't import due to cycle.

var newCompressorFuncs = map[Type]func(maxSize int64) (Compressor, error){
	TypeNone: func(int64) (Compressor, error) {
		return NewNoCompressor(), nil
	},
	TypeGzip: func(maxSize int64) (Compressor, error) {
		return NewGzipCompressor(maxSize)
	},
	TypeZstd: func(maxSize int64) (Compressor, error) {
		return NewZstdCompressor(maxSize)
	},
}

func TestCompressDecompress(t *testing.T) {
	for compressionType, newCompressorFunc := range newCompressorFuncs {
		t.Run(compressionType.String(), func(t *testing.T) {
			data := make([]byte, 4096)
			for i := 0; i < len(data); i++ {
				data[i] = byte(rand.Intn(256)) // #nosec G404
			}

			data2 := make([]byte, 4096)
			for i := 0; i < len(data); i++ {
				data2[i] = byte(rand.Intn(256)) // #nosec G404
			}

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

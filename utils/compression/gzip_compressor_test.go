// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/units"
)

func TestGzipCompressDecompress(t *testing.T) {
	data := make([]byte, 4096)
	for i := 0; i < len(data); i++ {
		data[i] = byte(rand.Intn(256)) // #nosec G404
	}

	data2 := make([]byte, 4096)
	for i := 0; i < len(data); i++ {
		data2[i] = byte(rand.Intn(256)) // #nosec G404
	}

	compressor, err := NewGzipCompressor(2 * units.MiB)
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

	nonGzipData := []byte{1, 2, 3}
	_, err = compressor.Decompress(nonGzipData)
	require.Error(t, err)
}

func TestGzipSizeLimiting(t *testing.T) {
	data := make([]byte, 3*units.MiB)
	compressor, err := NewGzipCompressor(2 * units.MiB)
	require.NoError(t, err)

	_, err = compressor.Compress(data) // should be too large
	require.Error(t, err)

	compressor2, err := NewGzipCompressor(4 * units.MiB)
	require.NoError(t, err)

	dataCompressed, err := compressor2.Compress(data)
	require.NoError(t, err)

	_, err = compressor.Decompress(dataCompressed) // should be too large
	require.Error(t, err)
}

// Attempts to create gzip compressor with math.MaxInt64
// which leads to undefined decompress behavior due to integer overflow
// in limit reader creation.
func TestNewGzipCompressorWithInvalidLimit(t *testing.T) {
	require := require.New(t)
	_, err := NewGzipCompressor(math.MaxInt64)
	require.ErrorIs(err, ErrInvalidMaxSizeGzipCompressor)
}

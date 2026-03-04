// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoCompressor(t *testing.T) {
	require := require.New(t)

	data := []byte{1, 2, 3}
	compressor := NewNoCompressor()
	compressedBytes, err := compressor.Compress(data)
	require.NoError(err)
	require.Equal(data, compressedBytes)

	decompressedBytes, err := compressor.Decompress(compressedBytes)
	require.NoError(err)
	require.Equal(data, decompressedBytes)
}

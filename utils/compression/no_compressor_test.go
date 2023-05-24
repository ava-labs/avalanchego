// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoCompressor(t *testing.T) {
	data := []byte{1, 2, 3}
	compressor := NewNoCompressor()
	compressedBytes, err := compressor.Compress(data)
	require.NoError(t, err)
	require.EqualValues(t, data, compressedBytes)

	decompressedBytes, err := compressor.Decompress(compressedBytes)
	require.NoError(t, err)
	require.EqualValues(t, data, decompressedBytes)
}

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoCompressor(t *testing.T) {
	data := []byte{1, 2, 3}
	compressor := NewNoCompressor()
	compressedBytes, err := compressor.Compress(data)
	assert.NoError(t, err)
	assert.EqualValues(t, data, compressedBytes)

	decompressedBytes, err := compressor.Decompress(compressedBytes)
	assert.NoError(t, err)
	assert.EqualValues(t, data, decompressedBytes)
}

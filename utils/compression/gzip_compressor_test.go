// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
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

	compressor := NewGzipCompressor()

	dataCompressed, err := compressor.Compress(data)
	assert.NoError(t, err)

	data2Compressed, err := compressor.Compress(data2)
	assert.NoError(t, err)

	dataDecompressed, err := compressor.Decompress(dataCompressed)
	assert.NoError(t, err)
	assert.EqualValues(t, data, dataDecompressed)

	data2Decompressed, err := compressor.Decompress(data2Compressed)
	assert.NoError(t, err)
	assert.EqualValues(t, data2, data2Decompressed)

	dataDecompressed, err = compressor.Decompress(dataCompressed)
	assert.NoError(t, err)
	assert.EqualValues(t, data, dataDecompressed)

	nonGzipData := []byte{1, 2, 3}
	_, err = compressor.Decompress(nonGzipData)
	assert.Error(t, err)
}

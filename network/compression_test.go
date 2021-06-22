package network

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompressDecompress(t *testing.T) {
	data := make([]byte, 4096)
	for i := 0; i < len(data); i++ {
		data[i] = byte(rand.Intn(256))
	}

	compressor := NewGzipCompressor(minCompressSize)
	compressedBytes, err := compressor.Compress(data)
	assert.NoError(t, err)

	decompressedBytes, err := compressor.Decompress(compressedBytes)
	assert.NoError(t, err)
	assert.EqualValues(t, data, decompressedBytes)
}

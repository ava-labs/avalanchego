package network

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompressDecompress(t *testing.T) {
	data := []byte(randomString(1000))

	compressor := NewCompressor()
	compressedBytes, err := compressor.Compress(data)
	assert.NoError(t, err)

	decompressedBytes, err := compressor.Decompress(compressedBytes)
	assert.NoError(t, err)
	assert.EqualValues(t, data, decompressedBytes)
}

func TestGzipCompressor_IsCompressable(t *testing.T) {
	compressor := NewCompressor()
	data := "abc123"
	assert.False(t, compressor.IsCompressable([]byte(data)))

	data = randomString(1000)
	assert.True(t, compressor.IsCompressable([]byte(data)))
}

func TestGzipCompressor_IsCompressed(t *testing.T) {
	compressor := NewCompressor()
	data := randomString(128)
	dataBytes := []byte(data)
	assert.False(t, compressor.IsCompressed(dataBytes))

	cmpBytes, err := compressor.Compress(dataBytes)
	assert.NoError(t, err)
	assert.True(t, compressor.IsCompressed(cmpBytes))
}

func randomString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 !%$*#@|/.,<>?[]{}-=_+()&^")

	s := make([]rune, n)
	for i := range s {
		randIndex := rand.Intn(len(letters))
		s[i] = letters[randIndex]
	}
	return string(s)
}

package network

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompressDecompress(t *testing.T) {
	data := []byte(randomString(1000))
	data2 := []byte(randomString(1000))

	compressor := NewCompressor()
	compressedBytes, err := compressor.Compress(data)
	assert.NoError(t, err)

	compressedBytes2, err := compressor.Compress(data2)
	assert.NoError(t, err)

	decompressedBytes, err := compressor.Decompress(compressedBytes)
	assert.NoError(t, err)
	assert.EqualValues(t, data, decompressedBytes)

	decompressedBytes2, err := compressor.Decompress(compressedBytes2)
	assert.NoError(t, err)
	assert.EqualValues(t, data2, decompressedBytes2)
}

func TestGzipCompressor_IsCompressable(t *testing.T) {
	compressor := NewCompressor()
	data := "abc123"
	assert.False(t, compressor.IsCompressable([]byte(data)))

	data = randomString(1000)
	assert.True(t, compressor.IsCompressable([]byte(data)))
}

func randomString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 !%$*#@|/.,<>?[]{}-=_+()&^")
	lettersLen := big.NewInt(int64(len(letters)))
	s := make([]rune, n)
	for i := range s {
		randIndex, _ := rand.Int(rand.Reader, lettersLen)
		s[i] = letters[randIndex.Int64()]
	}
	return string(s)
}

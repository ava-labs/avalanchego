package network

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompressDecompress(t *testing.T) {
	data := []byte(randomString(1000))

	compressor := NewGzipCompressor(minCompressSize)
	compressedBytes, err := compressor.Compress(data)
	assert.NoError(t, err)

	decompressedBytes, err := compressor.Decompress(compressedBytes)
	assert.NoError(t, err)
	assert.EqualValues(t, data, decompressedBytes)
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

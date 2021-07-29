// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"sync"

	"github.com/ava-labs/avalanchego/utils"
)

var errTooLarge = errors.New("message too large")

// gzipCompressor implements Compressor
type gzipCompressor struct {
	maxSize int64

	lock sync.Mutex

	writeBuffer *bytes.Buffer
	gzipWriter  *gzip.Writer

	bytesReader *bytes.Reader
	gzipReader  *gzip.Reader
}

// Compress [msg] and returns the compressed bytes.
func (g *gzipCompressor) Compress(msg []byte) ([]byte, error) {
	if int64(len(msg)) > g.maxSize {
		return nil, errTooLarge
	}

	g.lock.Lock()
	defer g.lock.Unlock()

	g.writeBuffer.Reset()
	g.gzipWriter.Reset(g.writeBuffer)
	if _, err := g.gzipWriter.Write(msg); err != nil {
		return nil, err
	}
	if err := g.gzipWriter.Close(); err != nil {
		return nil, err
	}

	compressed := g.writeBuffer.Bytes()
	compressedCopy := utils.CopyBytes(compressed)
	return compressedCopy, nil
}

// Decompress decompresses [msg].
func (g *gzipCompressor) Decompress(msg []byte) ([]byte, error) {
	g.lock.Lock()
	defer g.lock.Unlock()

	g.bytesReader.Reset(msg)
	if err := g.gzipReader.Reset(g.bytesReader); err != nil {
		return nil, err
	}
	limitedReader := io.LimitReader(g.gzipReader, g.maxSize+1)
	decompressed, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(decompressed)) > g.maxSize {
		return nil, errTooLarge
	}
	return decompressed, g.gzipReader.Close()
}

// NewGzipCompressor returns a new gzip Compressor that compresses
func NewGzipCompressor(maxSize int64) Compressor {
	var buf bytes.Buffer
	return &gzipCompressor{
		maxSize: maxSize,

		writeBuffer: &buf,
		gzipWriter:  gzip.NewWriter(&buf),

		bytesReader: &bytes.Reader{},
		gzipReader:  &gzip.Reader{},
	}
}

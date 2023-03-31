// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"golang.org/x/exp/slices"
)

var (
	_ Compressor = (*gzipCompressor)(nil)

	ErrInvalidMaxSizeGzipCompressor = errors.New("invalid gzip compressor max size")
)

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
		return nil, fmt.Errorf("msg length (%d) > maximum msg length (%d)", len(msg), g.maxSize)
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
	compressedCopy := slices.Clone(compressed)
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

	// We allow [io.LimitReader] to read up to [g.maxSize + 1] bytes, so that if
	// the decompressed payload is greater than the maximum size, this function
	// will return the appropriate error instead of an incomplete byte slice.
	limitedReader := io.LimitReader(g.gzipReader, g.maxSize+1)

	decompressed, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(decompressed)) > g.maxSize {
		return nil, fmt.Errorf("msg length > maximum msg length (%d)", g.maxSize)
	}
	return decompressed, g.gzipReader.Close()
}

// NewGzipCompressor returns a new gzip Compressor that compresses
func NewGzipCompressor(maxSize int64) (Compressor, error) {
	if maxSize == math.MaxInt64 {
		// "Decompress" creates "io.LimitReader" with max size + 1:
		// if the max size + 1 overflows, "io.LimitReader" reads nothing
		// returning 0 byte for the decompress call
		// require max size <math.MaxInt64 to prevent int64 overflows
		return nil, ErrInvalidMaxSizeGzipCompressor
	}

	var buf bytes.Buffer
	return &gzipCompressor{
		maxSize: maxSize,

		writeBuffer: &buf,
		gzipWriter:  gzip.NewWriter(&buf),

		bytesReader: &bytes.Reader{},
		gzipReader:  &gzip.Reader{},
	}, nil
}

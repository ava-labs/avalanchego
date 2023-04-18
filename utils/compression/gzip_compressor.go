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
)

var (
	_ Compressor = (*gzipCompressor)(nil)

	ErrInvalidMaxSizeCompressor = errors.New("invalid gzip compressor max size")
	ErrDecompressedMsgTooLarge  = errors.New("decompressed msg too large")
	ErrMsgTooLarge              = errors.New("msg too large to be compressed")
)

type gzipCompressor struct {
	maxSize        int64
	gzipWriterPool sync.Pool
}

// Compress [msg] and returns the compressed bytes.
func (g *gzipCompressor) Compress(msg []byte) ([]byte, error) {
	if int64(len(msg)) > g.maxSize {
		return nil, fmt.Errorf("%w: (%d) > (%d)", ErrMsgTooLarge, len(msg), g.maxSize)
	}

	var writeBuffer bytes.Buffer
	gzipWriter := g.gzipWriterPool.Get().(*gzip.Writer)
	gzipWriter.Reset(&writeBuffer)
	defer g.gzipWriterPool.Put(gzipWriter)

	if _, err := gzipWriter.Write(msg); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	return writeBuffer.Bytes(), nil
}

// Decompress decompresses [msg].
func (g *gzipCompressor) Decompress(msg []byte) ([]byte, error) {
	bytesReader := bytes.NewReader(msg)
	gzipReader, err := gzip.NewReader(bytesReader)
	if err != nil {
		return nil, err
	}

	// We allow [io.LimitReader] to read up to [g.maxSize + 1] bytes, so that if
	// the decompressed payload is greater than the maximum size, this function
	// will return the appropriate error instead of an incomplete byte slice.
	limitedReader := io.LimitReader(gzipReader, g.maxSize+1)

	decompressed, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(decompressed)) > g.maxSize {
		return nil, fmt.Errorf("%w: (%d) > (%d)", ErrDecompressedMsgTooLarge, len(decompressed), g.maxSize)
	}
	return decompressed, gzipReader.Close()
}

// NewGzipCompressor returns a new gzip Compressor that compresses
func NewGzipCompressor(maxSize int64) (Compressor, error) {
	if maxSize == math.MaxInt64 {
		// "Decompress" creates "io.LimitReader" with max size + 1:
		// if the max size + 1 overflows, "io.LimitReader" reads nothing
		// returning 0 byte for the decompress call
		// require max size <math.MaxInt64 to prevent int64 overflows
		return nil, ErrInvalidMaxSizeCompressor
	}

	return &gzipCompressor{
		maxSize: maxSize,
		gzipWriterPool: sync.Pool{
			New: func() interface{} {
				return gzip.NewWriter(nil)
			},
		},
	}, nil
}

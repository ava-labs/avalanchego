// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/DataDog/zstd"
)

var (
	_ Compressor = (*zstdCompressor)(nil)

	ErrInvalidMaxSizeCompressor = errors.New("invalid compressor max size")
	ErrDecompressedMsgTooLarge  = errors.New("decompressed msg too large")
	ErrMsgTooLarge              = errors.New("msg too large to be compressed")
)

func NewZstdCompressor(maxSize int64) (Compressor, error) {
	return NewZstdCompressorWithLevel(maxSize, zstd.DefaultCompression)
}

func NewZstdCompressorWithLevel(maxSize int64, level int) (Compressor, error) {
	if maxSize == math.MaxInt64 {
		// "Decompress" creates "io.LimitReader" with max size + 1:
		// if the max size + 1 overflows, "io.LimitReader" reads nothing
		// returning 0 byte for the decompress call
		// require max size < math.MaxInt64 to prevent int64 overflows
		return nil, ErrInvalidMaxSizeCompressor
	}
	return &zstdCompressor{
		maxSize: maxSize,
		level:   level,
	}, nil
}

type zstdCompressor struct {
	maxSize int64
	level   int
}

func (z *zstdCompressor) Compress(msg []byte) ([]byte, error) {
	if int64(len(msg)) > z.maxSize {
		return nil, fmt.Errorf("%w: (%d) > (%d)", ErrMsgTooLarge, len(msg), z.maxSize)
	}
	return zstd.CompressLevel(nil, msg, z.level)
}

func (z *zstdCompressor) Decompress(msg []byte) ([]byte, error) {
	reader := zstd.NewReader(bytes.NewReader(msg))
	defer reader.Close()

	// We allow [io.LimitReader] to read up to [z.maxSize + 1] bytes, so that if
	// the decompressed payload is greater than the maximum size, this function
	// will return the appropriate error instead of an incomplete byte slice.
	limitReader := io.LimitReader(reader, z.maxSize+1)
	decompressed, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, err
	}
	if int64(len(decompressed)) > z.maxSize {
		return nil, fmt.Errorf("%w: (%d) > (%d)", ErrDecompressedMsgTooLarge, len(decompressed), z.maxSize)
	}
	return decompressed, nil
}

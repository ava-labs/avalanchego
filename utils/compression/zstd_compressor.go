// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"fmt"

	"github.com/DataDog/zstd"
)

var _ Compressor = (*zstdCompressor)(nil)

func NewZstdCompressor(maxSize int64) Compressor {
	return &zstdCompressor{
		maxSize: maxSize,
	}
}

type zstdCompressor struct {
	maxSize int64
}

func (z *zstdCompressor) Compress(msg []byte) ([]byte, error) {
	if int64(len(msg)) > z.maxSize {
		return nil, fmt.Errorf("%w: (%d) > (%d)", ErrMsgTooLarge, len(msg), z.maxSize)
	}
	return zstd.Compress(nil, msg)
}

func (z *zstdCompressor) Decompress(msg []byte) ([]byte, error) {
	decompressed, err := zstd.Decompress(nil, msg)
	if err != nil {
		return nil, err
	}
	if int64(len(decompressed)) > z.maxSize {
		return nil, fmt.Errorf("%w: (%d) > (%d)", ErrDecompressedMsgTooLarge, len(decompressed), z.maxSize)
	}
	return decompressed, nil
}

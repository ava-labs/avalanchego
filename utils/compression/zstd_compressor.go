// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"fmt"
	"math"

	"github.com/DataDog/zstd"
)

var _ Compressor = (*zstdCompressor)(nil)

func NewZstdCompressor() Compressor {
	return &zstdCompressor{
		maxSize: math.MaxUint32, // TODO fix
	}
}

type zstdCompressor struct {
	maxSize int64
}

func (c *zstdCompressor) Compress(msg []byte) ([]byte, error) {
	if int64(len(msg)) > c.maxSize {
		return nil, fmt.Errorf("msg length (%d) > maximum msg length (%d)", len(msg), c.maxSize)
	}
	return zstd.Compress(nil, msg)
}

func (c *zstdCompressor) Decompress(msg []byte) ([]byte, error) {
	return zstd.Decompress(nil, msg)
}

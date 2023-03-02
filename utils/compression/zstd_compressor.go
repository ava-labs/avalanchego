// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

import (
	"github.com/DataDog/zstd"
)

var _ Compressor = (*zstdCompressor)(nil)

func NewZstdCompressor() Compressor {
	return &zstdCompressor{}
}

type zstdCompressor struct {
}

func (c *zstdCompressor) Compress(msg []byte) ([]byte, error) {
	return zstd.Compress(nil, msg)
}

func (c *zstdCompressor) Decompress(msg []byte) ([]byte, error) {
	return zstd.Decompress(nil, msg)
}

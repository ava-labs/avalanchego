// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

var _ Compressor = (*noCompressor)(nil)

type noCompressor struct{}

func (*noCompressor) Compress(msg []byte) ([]byte, error) {
	return msg, nil
}

func (*noCompressor) Decompress(msg []byte) ([]byte, error) {
	return msg, nil
}

func NewNoCompressor() Compressor {
	return &noCompressor{}
}

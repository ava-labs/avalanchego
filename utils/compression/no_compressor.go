// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

var _ Compressor = (*noCompressor)(nil)

type noCompressor struct{}

// Compress returns [msg]
func (*noCompressor) Compress(msg []byte) ([]byte, error) {
	return msg, nil
}

// Decompress returns [msg].
func (*noCompressor) Decompress(msg []byte) ([]byte, error) {
	return msg, nil
}

// NewNoCompressor returns a Compressor that does nothing
func NewNoCompressor() Compressor {
	return &noCompressor{}
}

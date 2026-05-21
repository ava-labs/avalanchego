// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

// Compressor compress and decompresses messages.
// Decompress is the inverse of Compress.
// Decompress(Compress(msg)) == msg.
type Compressor interface {
	Compress([]byte) ([]byte, error)
	Decompress([]byte) ([]byte, error)
}

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package compression

type Type byte

const (
	NoCompression Type = iota + 1
	GzipCompression
	ZstdCompression
)

// Compressor compresss and decompresses messages.
// Decompress is the inverse of Compress.
// Decompress(Compress(msg)) == msg.
type Compressor interface {
	Compress([]byte) ([]byte, error)
	Decompress([]byte) ([]byte, error)
}

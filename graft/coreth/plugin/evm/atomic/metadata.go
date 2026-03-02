// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

type Metadata struct {
	id            ids.ID // The ID of this data
	unsignedBytes []byte // Unsigned byte representation of this data
	bytes         []byte // Byte representation of this data
}

// Initialize set the bytes and ID
func (md *Metadata) Initialize(unsignedBytes, bytes []byte) {
	md.id = hashing.ComputeHash256Array(bytes)
	md.unsignedBytes = unsignedBytes
	md.bytes = bytes
}

// ID returns the unique ID of this data
func (md *Metadata) ID() ids.ID {
	return md.id
}

// UnsignedBytes returns the unsigned binary representation of this data
func (md *Metadata) Bytes() []byte {
	return md.unsignedBytes
}

// Bytes returns the binary representation of this data
func (md *Metadata) SignedBytes() []byte {
	return md.bytes
}

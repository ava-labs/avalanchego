// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errNilMetadata           = errors.New("nil metadata is not valid")
	errMetadataNotInitialize = errors.New("metadata was never initialized and is not valid")

	_ verify.Verifiable = &Metadata{}
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
func (md *Metadata) ID() ids.ID { return md.id }

// UnsignedBytes returns the unsigned binary representation of this data
func (md *Metadata) UnsignedBytes() []byte { return md.unsignedBytes }

// Bytes returns the binary representation of this data
func (md *Metadata) Bytes() []byte { return md.bytes }

func (md *Metadata) Verify() error {
	switch {
	case md == nil:
		return errNilMetadata
	case md.id == ids.Empty:
		return errMetadataNotInitialize
	default:
		return nil
	}
}

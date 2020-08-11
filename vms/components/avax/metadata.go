// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/hashing"
)

var (
	errNilMetadata           = errors.New("nil metadata is not valid")
	errMetadataNotInitialize = errors.New("metadata was never initialized and is not valid")
)

// Metadata ...
type Metadata struct {
	id    ids.ID // The ID of this data
	bytes []byte // Byte representation of this data
}

// Initialize set the bytes and ID
func (md *Metadata) Initialize(bytes []byte) {
	md.id = ids.NewID(hashing.ComputeHash256Array(bytes))
	md.bytes = bytes
}

// ID returns the unique ID of this data
func (md *Metadata) ID() ids.ID { return md.id }

// Bytes returns the binary representation of this data
func (md *Metadata) Bytes() []byte { return md.bytes }

// Verify implements the verify.Verifiable interface
func (md *Metadata) Verify() error {
	switch {
	case md == nil:
		return errNilMetadata
	case md.id.IsZero():
		return errMetadataNotInitialize
	default:
		return nil
	}
}

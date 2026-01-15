// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Payload = (*Hash)(nil)

type Hash struct {
	Hash ids.ID `serialize:"true"`

	bytes []byte
}

// NewHash creates a new *Hash and initializes it.
func NewHash(hash ids.ID) (*Hash, error) {
	bhp := &Hash{
		Hash: hash,
	}
	return bhp, initialize(bhp)
}

// ParseHash converts a slice of bytes into an initialized Hash.
func ParseHash(b []byte) (*Hash, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*Hash)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewHash or Parse.
func (b *Hash) Bytes() []byte {
	return b.bytes
}

func (b *Hash) initialize(bytes []byte) {
	b.bytes = bytes
}

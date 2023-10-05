// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Payload = (*BlockHash)(nil)

type BlockHash struct {
	BlockHash ids.ID `serialize:"true"`

	bytes []byte
}

// NewBlockHash creates a new *BlockHash and initializes it.
func NewBlockHash(blockHash ids.ID) (*BlockHash, error) {
	bhp := &BlockHash{
		BlockHash: blockHash,
	}
	return bhp, initialize(bhp)
}

// ParseBlockHash converts a slice of bytes into an initialized BlockHash.
func ParseBlockHash(b []byte) (*BlockHash, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*BlockHash)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, payloadIntf)
	}
	return payload, nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewBlockHash or ParseBlockHash.
func (b *BlockHash) Bytes() []byte {
	return b.bytes
}

func (b *BlockHash) initialize(bytes []byte) {
	b.bytes = bytes
}

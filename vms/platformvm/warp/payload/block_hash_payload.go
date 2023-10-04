// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var _ byteSetter = (*BlockHash)(nil)

// BlockHash includes the block hash
type BlockHash struct {
	BlockHash ids.ID `serialize:"true"`

	bytes []byte
}

// NewBlockHash creates a new *BlockHash and initializes it.
func NewBlockHash(blockHash ids.ID) (*BlockHash, error) {
	bhp := &BlockHash{
		BlockHash: blockHash,
	}
	return bhp, bhp.initialize()
}

// ParseBlockHash converts a slice of bytes into an initialized
// BlockHash
func ParseBlockHash(b []byte) (*BlockHash, error) {
	var unmarshalledPayloadIntf any
	if _, err := c.Unmarshal(b, &unmarshalledPayloadIntf); err != nil {
		return nil, err
	}
	payload, ok := unmarshalledPayloadIntf.(*BlockHash)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, unmarshalledPayloadIntf)
	}
	payload.bytes = b
	return payload, nil
}

// initialize recalculates the result of Bytes().
func (b *BlockHash) initialize() error {
	payloadIntf := any(b)
	bytes, err := c.Marshal(codecVersion, &payloadIntf)
	if err != nil {
		return fmt.Errorf("couldn't marshal block hash payload: %w", err)
	}
	b.bytes = bytes
	return nil
}

func (b *BlockHash) setBytes(bytes []byte) {
	b.bytes = bytes
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewBlockHash or ParseBlockHash.
func (b *BlockHash) Bytes() []byte {
	return b.bytes
}

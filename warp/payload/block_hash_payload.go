// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package payload

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// BlockHashPayload includes the block hash
type BlockHashPayload struct {
	BlockHash common.Hash `serialize:"true"`

	bytes []byte
}

// NewBlockHashPayload creates a new *BlockHashPayload and initializes it.
func NewBlockHashPayload(blockHash common.Hash) (*BlockHashPayload, error) {
	bhp := &BlockHashPayload{
		BlockHash: blockHash,
	}
	return bhp, bhp.initialize()
}

// ParseBlockHashPayload converts a slice of bytes into an initialized
// BlockHashPayload
func ParseBlockHashPayload(b []byte) (*BlockHashPayload, error) {
	var unmarshalledPayloadIntf any
	if _, err := c.Unmarshal(b, &unmarshalledPayloadIntf); err != nil {
		return nil, err
	}
	payload, ok := unmarshalledPayloadIntf.(*BlockHashPayload)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errWrongType, unmarshalledPayloadIntf)
	}
	payload.bytes = b
	return payload, nil
}

// initialize recalculates the result of Bytes().
func (b *BlockHashPayload) initialize() error {
	payloadIntf := any(b)
	bytes, err := c.Marshal(codecVersion, &payloadIntf)
	if err != nil {
		return fmt.Errorf("couldn't marshal block hash payload: %w", err)
	}
	b.bytes = bytes
	return nil
}

// Bytes returns the binary representation of this payload. It assumes that the
// payload is initialized from either NewBlockHashPayload or ParseBlockHashPayload.
func (b *BlockHashPayload) Bytes() []byte {
	return b.bytes
}

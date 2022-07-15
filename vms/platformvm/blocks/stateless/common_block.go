// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// CommonBlock contains fields and methods common to all blocks in this VM.
type CommonBlock struct {
	PrntID ids.ID `serialize:"true" json:"parentID"` // parent's ID
	Hght   uint64 `serialize:"true" json:"height"`   // This block's height. The genesis block is at height 0.

	id    ids.ID
	bytes []byte
}

func (b *CommonBlock) initialize(bytes []byte) error {
	b.id = hashing.ComputeHash256Array(bytes)
	b.bytes = bytes
	return nil
}

// ID returns the ID of this block
func (b *CommonBlock) ID() ids.ID { return b.id }

// Bytes returns the binary representation of this block
func (b *CommonBlock) Bytes() []byte { return b.bytes }

// Parent returns this block's parent's ID
func (b *CommonBlock) Parent() ids.ID { return b.PrntID }

// Height returns this block's height. The genesis block has height 0.
func (b *CommonBlock) Height() uint64 { return b.Hght }

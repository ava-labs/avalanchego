// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// CommonBlock contains fields and methods common to all blocks in this VM.
type CommonBlock struct {
	// parent's ID
	PrntID ids.ID `serialize:"true" json:"parentID"`

	// This block's height. The genesis block is at height 0.
	Hght uint64 `serialize:"true" json:"height"`

	// Time this block was proposed at. Note that this
	// is serialized for blueberry blocks only
	BlkTimestamp uint64 `serialize:"false" blueberry:"true" json:"time"`

	// Codec version used to serialized/deserialize the block
	version uint16

	id    ids.ID
	bytes []byte
}

func (b *CommonBlock) initialize(version uint16, bytes []byte) error {
	b.id = hashing.ComputeHash256Array(bytes)
	b.bytes = bytes
	b.version = version
	return nil
}

func (b *CommonBlock) ID() ids.ID     { return b.id }
func (b *CommonBlock) Parent() ids.ID { return b.PrntID }
func (b *CommonBlock) Bytes() []byte  { return b.bytes }
func (b *CommonBlock) Height() uint64 { return b.Hght }

func (b *CommonBlock) Version() uint16 { return b.version }

func (b *CommonBlock) UnixTimestamp() int64 {
	return int64(b.BlkTimestamp)
}

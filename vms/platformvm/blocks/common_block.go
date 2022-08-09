// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

type BlueberryCommonBlock struct {
	ApricotCommonBlock `serialize:"true"`

	// Time this block was proposed at. Note that this
	// is serialized for blueberry blocks only
	BlkTimestamp uint64 `serialize:"true" json:"time"`
}

func (b *BlueberryCommonBlock) BlockTimestamp() time.Time {
	return time.Unix(int64(b.BlkTimestamp), 0)
}

// ApricotCommonBlock contains fields and methods common to all blocks in this VM.
type ApricotCommonBlock struct {
	// parent's ID
	PrntID ids.ID `serialize:"true" json:"parentID"`

	// This block's height. The genesis block is at height 0.
	Hght uint64 `serialize:"true" json:"height"`

	id    ids.ID
	bytes []byte
}

func (b *ApricotCommonBlock) initialize(bytes []byte) {
	b.id = hashing.ComputeHash256Array(bytes)
	b.bytes = bytes
}

func (b *ApricotCommonBlock) ID() ids.ID     { return b.id }
func (b *ApricotCommonBlock) Parent() ids.ID { return b.PrntID }
func (b *ApricotCommonBlock) Bytes() []byte  { return b.bytes }
func (b *ApricotCommonBlock) Height() uint64 { return b.Hght }

func (b *ApricotCommonBlock) BlockTimestamp() time.Time {
	return time.Unix(0, 0)
}

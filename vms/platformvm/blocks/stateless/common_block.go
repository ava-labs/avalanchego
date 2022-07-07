// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// CommonBlock contains fields and methods common to all blocks in this VM.
type CommonBlock struct {
	PrntID ids.ID `serialize:"true" json:"parentID"` // parent's ID
	Hght   uint64 `serialize:"true" json:"height"`   // This block's height. The genesis block is at height 0.

	id    ids.ID
	bytes []byte

	// TODO consolidate these interfaces?
	BlockVerifier
	BlockAcceptor
	BlockRejector
	Statuser
	Timestamper
}

func (b *CommonBlock) Initialize(bytes []byte) error {
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

func (b *CommonBlock) Status() choices.Status {
	return b.Statuser.Status(b.ID())
}

func (b *CommonBlock) Timestamp() time.Time {
	return b.Timestamper.Timestamp(b.ID())
}

func (b *CommonBlock) Sync(
	verifier BlockVerifier,
	acceptor BlockAcceptor,
	rejector BlockRejector,
	statuser Statuser,
	timestamper Timestamper,
) {
	b.BlockVerifier = verifier
	b.BlockAcceptor = acceptor
	b.BlockRejector = rejector
	b.Statuser = statuser
	b.Timestamper = timestamper
}

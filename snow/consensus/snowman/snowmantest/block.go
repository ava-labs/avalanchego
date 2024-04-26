// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowmantest

import (
	"cmp"
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils"
)

const GenesisHeight uint64 = 0

var (
	_ utils.Sortable[*Block] = (*Block)(nil)

	GenesisID        = ids.GenerateTestID()
	GenesisTimestamp = time.Unix(1, 0)
	GenesisBytes     = GenesisID[:]
	Genesis          = &Block{
		TestDecidable: choices.TestDecidable{
			IDV:     GenesisID,
			StatusV: choices.Accepted,
		},
		HeightV:    GenesisHeight,
		TimestampV: GenesisTimestamp,
		BytesV:     GenesisBytes,
	}
)

func BuildChildBlock(parent *Block) *Block {
	blkID := ids.GenerateTestID()
	return &Block{
		TestDecidable: choices.TestDecidable{
			IDV:     blkID,
			StatusV: choices.Processing,
		},
		ParentV:    parent.ID(),
		HeightV:    parent.Height() + 1,
		TimestampV: parent.Timestamp().Add(time.Second),
		BytesV:     blkID[:],
	}
}

func BuildChain(parent *Block, length int) []*Block {
	chain := make([]*Block, length)
	for i := range chain {
		parent = BuildChildBlock(parent)
		chain[i] = parent
	}
	return chain
}

type Block struct {
	choices.TestDecidable

	ParentV    ids.ID
	HeightV    uint64
	TimestampV time.Time
	VerifyV    error
	BytesV     []byte
}

func (b *Block) Parent() ids.ID {
	return b.ParentV
}

func (b *Block) Height() uint64 {
	return b.HeightV
}

func (b *Block) Timestamp() time.Time {
	return b.TimestampV
}

func (b *Block) Verify(context.Context) error {
	return b.VerifyV
}

func (b *Block) Bytes() []byte {
	return b.BytesV
}

func (b *Block) Compare(other *Block) int {
	return cmp.Compare(b.HeightV, other.HeightV)
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

var _ Block = (*StandardBlock)(nil)

type StandardBlock struct {
	// parent's ID
	PrntID ids.ID `serialize:"true" json:"parentID"`
	// This block's height. The genesis block is at height 0.
	Hght uint64 `serialize:"true" json:"height"`
	Time uint64 `serialize:"true" json:"time"`
	Root ids.ID `serialize:"true" json:"merkleRoot"`
	// List of transactions contained in this block.
	Transactions []*txs.Tx `serialize:"true" json:"txs"`

	BlockID ids.ID `json:"id"`
	bytes   []byte
}

func (b *StandardBlock) initialize(bytes []byte, cm codec.Manager) error {
	b.BlockID = hashing.ComputeHash256Array(bytes)
	b.bytes = bytes
	for _, tx := range b.Transactions {
		if err := tx.Initialize(cm); err != nil {
			return fmt.Errorf("failed to initialize tx: %w", err)
		}
	}
	return nil
}

func (b *StandardBlock) InitCtx(ctx *snow.Context) {
	for _, tx := range b.Transactions {
		tx.Unsigned.InitCtx(ctx)
	}
}

func (b *StandardBlock) ID() ids.ID {
	return b.BlockID
}

func (b *StandardBlock) Parent() ids.ID {
	return b.PrntID
}

func (b *StandardBlock) Height() uint64 {
	return b.Hght
}

func (b *StandardBlock) Timestamp() time.Time {
	return time.Unix(int64(b.Time), 0)
}

func (b *StandardBlock) MerkleRoot() ids.ID {
	return b.Root
}

func (b *StandardBlock) Txs() []*txs.Tx {
	return b.Transactions
}

func (b *StandardBlock) Bytes() []byte {
	return b.bytes
}

func NewStandardBlock(
	parentID ids.ID,
	height uint64,
	timestamp time.Time,
	txs []*txs.Tx,
	cm codec.Manager,
) (*StandardBlock, error) {
	blk := &StandardBlock{
		PrntID:       parentID,
		Hght:         height,
		Time:         uint64(timestamp.Unix()),
		Transactions: txs,
	}

	// We serialize this block as a pointer so that it can be deserialized into
	// a Block
	var blkIntf Block = blk
	bytes, err := cm.Marshal(CodecVersion, &blkIntf)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal block: %w", err)
	}

	blk.BlockID = hashing.ComputeHash256Array(bytes)
	blk.bytes = bytes
	return blk, nil
}

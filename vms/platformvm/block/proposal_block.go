// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ BanffBlock = (*BanffProposalBlock)(nil)
	_ Block      = (*ApricotProposalBlock)(nil)
)

type BanffProposalBlock struct {
	Time                 uint64             `serialize:"true" json:"time"`
	Transactions         []*txs.Tx          `serialize:"true" json:"txs"`
	ApricotProposalBlock `serialize:"true"` //nolint:embeddedstructfieldcheck
}

func (b *BanffProposalBlock) initialize(bytes []byte) error {
	if err := b.ApricotProposalBlock.initialize(bytes); err != nil {
		return err
	}
	for _, tx := range b.Transactions {
		if err := tx.Initialize(txs.Codec); err != nil {
			return fmt.Errorf("failed to initialize tx: %w", err)
		}
	}
	return nil
}

func (b *BanffProposalBlock) InitCtx(ctx *snow.Context) {
	for _, tx := range b.Transactions {
		tx.Unsigned.InitCtx(ctx)
	}
	b.ApricotProposalBlock.InitCtx(ctx)
}

func (b *BanffProposalBlock) Timestamp() time.Time {
	return time.Unix(int64(b.Time), 0)
}

func (b *BanffProposalBlock) Txs() []*txs.Tx {
	l := len(b.Transactions)
	txs := make([]*txs.Tx, l+1)
	copy(txs, b.Transactions)
	txs[l] = b.Tx
	return txs
}

func (b *BanffProposalBlock) Visit(v Visitor) error {
	return v.BanffProposalBlock(b)
}

func NewBanffProposalBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
	proposalTx *txs.Tx,
	decisionTxs []*txs.Tx,
) (*BanffProposalBlock, error) {
	blk := &BanffProposalBlock{
		Transactions: decisionTxs,
		Time:         uint64(timestamp.Unix()),
		ApricotProposalBlock: ApricotProposalBlock{
			CommonBlock: CommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
			Tx: proposalTx,
		},
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

type ApricotProposalBlock struct {
	CommonBlock `serialize:"true"`

	Tx *txs.Tx `serialize:"true" json:"tx"`
}

func (b *ApricotProposalBlock) initialize(bytes []byte) error {
	b.CommonBlock.initialize(bytes)
	if err := b.Tx.Initialize(txs.Codec); err != nil {
		return fmt.Errorf("failed to initialize tx: %w", err)
	}
	return nil
}

func (b *ApricotProposalBlock) InitCtx(ctx *snow.Context) {
	b.Tx.Unsigned.InitCtx(ctx)
}

func (b *ApricotProposalBlock) Txs() []*txs.Tx {
	return []*txs.Tx{b.Tx}
}

func (b *ApricotProposalBlock) Visit(v Visitor) error {
	return v.ApricotProposalBlock(b)
}

// NewApricotProposalBlock is kept for testing purposes only.
// Following Banff activation and subsequent code cleanup, Apricot Proposal blocks
// should be only verified (upon bootstrap), never created anymore
func NewApricotProposalBlock(
	parentID ids.ID,
	height uint64,
	tx *txs.Tx,
) (*ApricotProposalBlock, error) {
	blk := &ApricotProposalBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Tx: tx,
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

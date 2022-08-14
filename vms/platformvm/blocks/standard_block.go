// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"

	transactions "github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &BlueberryStandardBlock{}
	_ Block = &ApricotStandardBlock{}
)

func NewBlueberryStandardBlock(timestamp time.Time, parentID ids.ID, height uint64, txs []*transactions.Tx) (Block, error) {
	blk := &BlueberryStandardBlock{
		BlkTimestamp: uint64(timestamp.Unix()),
		ApricotStandardBlock: &ApricotStandardBlock{
			ApricotCommonBlock: ApricotCommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
			Transactions: txs,
		},
	}
	return blk, initialize(blk)
}

type BlueberryStandardBlock struct {
	BlkTimestamp uint64 `serialize:"true" json:"time"`

	*ApricotStandardBlock `serialize:"true"`
}

func (b *BlueberryStandardBlock) BlockTimestamp() time.Time {
	return time.Unix(int64(b.BlkTimestamp), 0)
}

func (b *BlueberryStandardBlock) Visit(v Visitor) error {
	return v.BlueberryStandardBlock(b)
}

func NewApricotStandardBlock(parentID ids.ID, height uint64, txs []*transactions.Tx) (Block, error) {
	blk := &ApricotStandardBlock{
		ApricotCommonBlock: ApricotCommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Transactions: txs,
	}
	return blk, initialize(blk)
}

type ApricotStandardBlock struct {
	ApricotCommonBlock `serialize:"true"`

	Transactions []*transactions.Tx `serialize:"true" json:"txs"`
}

func (b *ApricotStandardBlock) initialize(bytes []byte) error {
	b.ApricotCommonBlock.initialize(bytes)
	for _, tx := range b.Transactions {
		if err := tx.Sign(transactions.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
	}
	return nil
}

func (b *ApricotStandardBlock) Txs() []*transactions.Tx { return b.Transactions }

func (b *ApricotStandardBlock) Visit(v Visitor) error {
	return v.ApricotStandardBlock(b)
}

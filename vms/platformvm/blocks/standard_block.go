// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &BlueberryStandardBlock{}
	_ Block = &ApricotStandardBlock{}
)

// NewBlueberryStandardBlock assumes [txes] are initialized
func NewBlueberryStandardBlock(timestamp time.Time, parentID ids.ID, height uint64, txes []*txs.Tx) (Block, error) {
	res := &BlueberryStandardBlock{
		BlueberryCommonBlock: BlueberryCommonBlock{
			ApricotCommonBlock: ApricotCommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
			BlkTimestamp: uint64(timestamp.Unix()),
		},
		Transactions: txes,
	}

	return res, initialize(Block(res))
}

type BlueberryStandardBlock struct {
	BlueberryCommonBlock `serialize:"true"`

	Transactions []*txs.Tx `serialize:"true" json:"txs"`
}

func (b *BlueberryStandardBlock) initialize(bytes []byte) error {
	b.BlueberryCommonBlock.initialize(bytes)
	for _, tx := range b.Transactions {
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return fmt.Errorf("failed to initialize tx: %w", err)
		}
	}
	return nil
}

func (b *BlueberryStandardBlock) Txs() []*txs.Tx { return b.Transactions }

func (b *BlueberryStandardBlock) Visit(v Visitor) error {
	return v.BlueberryStandardBlock(b)
}

// NewApricotStandardBlock assumes [txes] are initialized
func NewApricotStandardBlock(parentID ids.ID, height uint64, txes []*txs.Tx) (Block, error) {
	res := &ApricotStandardBlock{
		ApricotCommonBlock: ApricotCommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Transactions: txes,
	}

	return res, initialize(Block(res))
}

type ApricotStandardBlock struct {
	ApricotCommonBlock `serialize:"true"`

	Transactions []*txs.Tx `serialize:"true" json:"txs"`
}

func (b *ApricotStandardBlock) initialize(bytes []byte) error {
	b.ApricotCommonBlock.initialize(bytes)
	for _, tx := range b.Transactions {
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
	}
	return nil
}

func (b *ApricotStandardBlock) Txs() []*txs.Tx { return b.Transactions }

func (b *ApricotStandardBlock) Visit(v Visitor) error {
	return v.ApricotStandardBlock(b)
}

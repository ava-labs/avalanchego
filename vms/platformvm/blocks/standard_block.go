// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/version"
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
	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(res)
	bytes, err := Codec.Marshal(Version, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}
	return res, res.initialize(version.BlueberryBlockVersion, bytes)
}

type BlueberryStandardBlock struct {
	BlueberryCommonBlock `serialize:"true"`

	Transactions []*txs.Tx `serialize:"true" json:"txs"`
}

func (b *BlueberryStandardBlock) initialize(version uint16, bytes []byte) error {
	if err := b.BlueberryCommonBlock.initialize(version, bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	for _, tx := range b.Transactions {
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
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
	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(res)
	bytes, err := Codec.Marshal(Version, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	return res, res.initialize(version.ApricotBlockVersion, bytes)
}

type ApricotStandardBlock struct {
	ApricotCommonBlock `serialize:"true"`

	Transactions []*txs.Tx `serialize:"true" json:"txs"`
}

func (b *ApricotStandardBlock) initialize(version uint16, bytes []byte) error {
	if err := b.ApricotCommonBlock.initialize(version, bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
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

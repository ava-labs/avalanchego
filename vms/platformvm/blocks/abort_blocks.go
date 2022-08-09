// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &BlueberryAbortBlock{}
	_ Block = &ApricotAbortBlock{}
)

func NewBlueberryAbortBlock(timestamp time.Time, parentID ids.ID, height uint64) (Block, error) {
	res := &BlueberryAbortBlock{
		BlueberryCommonBlock: BlueberryCommonBlock{
			ApricotCommonBlock: ApricotCommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
			BlkTimestamp: uint64(timestamp.Unix()),
		},
	}

	return res, initialize(Block(res))
}

type BlueberryAbortBlock struct {
	BlueberryCommonBlock `serialize:"true"`
}

func (b *BlueberryAbortBlock) initialize(bytes []byte) error {
	b.BlueberryCommonBlock.initialize(bytes)
	return nil
}

func (*BlueberryAbortBlock) Txs() []*txs.Tx { return nil }

func (b *BlueberryAbortBlock) Visit(v Visitor) error {
	return v.BlueberryAbortBlock(b)
}

func NewApricotAbortBlock(parentID ids.ID, height uint64) (Block, error) {
	res := &ApricotAbortBlock{
		ApricotCommonBlock: ApricotCommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
	}

	return res, initialize(Block(res))
}

type ApricotAbortBlock struct {
	ApricotCommonBlock `serialize:"true"`
}

func (b *ApricotAbortBlock) initialize(bytes []byte) error {
	b.ApricotCommonBlock.initialize(bytes)
	return nil
}

func (*ApricotAbortBlock) Txs() []*txs.Tx { return nil }

func (b *ApricotAbortBlock) Visit(v Visitor) error {
	return v.ApricotAbortBlock(b)
}

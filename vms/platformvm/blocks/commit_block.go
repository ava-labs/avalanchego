// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &BlueberryCommitBlock{}
	_ Block = &ApricotCommitBlock{}
)

func NewBlueberryCommitBlock(timestamp time.Time, parentID ids.ID, height uint64) (Block, error) {
	res := &BlueberryCommitBlock{
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

type BlueberryCommitBlock struct {
	BlueberryCommonBlock `serialize:"true"`
}

func (b *BlueberryCommitBlock) initialize(bytes []byte) error {
	b.BlueberryCommonBlock.initialize(bytes)
	return nil
}

func (*BlueberryCommitBlock) Txs() []*txs.Tx { return nil }

func (b *BlueberryCommitBlock) Visit(v Visitor) error {
	return v.BlueberryCommitBlock(b)
}

func NewApricotCommitBlock(parentID ids.ID, height uint64) (Block, error) {
	res := &ApricotCommitBlock{
		ApricotCommonBlock: ApricotCommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
	}

	return res, initialize(Block(res))
}

type ApricotCommitBlock struct {
	ApricotCommonBlock `serialize:"true"`
}

func (b *ApricotCommitBlock) initialize(bytes []byte) error {
	b.ApricotCommonBlock.initialize(bytes)
	return nil
}

func (*ApricotCommitBlock) Txs() []*txs.Tx { return nil }

func (b *ApricotCommitBlock) Visit(v Visitor) error {
	return v.ApricotCommitBlock(b)
}

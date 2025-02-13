// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ BanffBlock = (*BanffCommitBlock)(nil)
	_ Block      = (*ApricotCommitBlock)(nil)
)

type BanffCommitBlock struct {
	Time               uint64 `serialize:"true" json:"time"`
	ApricotCommitBlock `serialize:"true"`
}

func (b *BanffCommitBlock) Timestamp() time.Time {
	return time.Unix(int64(b.Time), 0)
}

func (b *BanffCommitBlock) Visit(v Visitor) error {
	return v.BanffCommitBlock(b)
}

func NewBanffCommitBlock(
	timestamp time.Time,
	parentID ids.ID,
	height uint64,
) (*BanffCommitBlock, error) {
	blk := &BanffCommitBlock{
		Time: uint64(timestamp.Unix()),
		ApricotCommitBlock: ApricotCommitBlock{
			CommonBlock: CommonBlock{
				PrntID: parentID,
				Hght:   height,
			},
		},
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

type ApricotCommitBlock struct {
	CommonBlock `serialize:"true"`
}

func (b *ApricotCommitBlock) initialize(bytes []byte) error {
	b.CommonBlock.initialize(bytes)
	return nil
}

func (*ApricotCommitBlock) InitCtx(*snow.Context) {}

func (*ApricotCommitBlock) Txs() []*txs.Tx {
	return nil
}

func (b *ApricotCommitBlock) Visit(v Visitor) error {
	return v.ApricotCommitBlock(b)
}

func NewApricotCommitBlock(
	parentID ids.ID,
	height uint64,
) (*ApricotCommitBlock, error) {
	blk := &ApricotCommitBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
	}
	return blk, initialize(blk, &blk.CommonBlock)
}

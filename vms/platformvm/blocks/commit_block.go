// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Block = &ApricotCommitBlock{}

type ApricotCommitBlock struct {
	CommonBlock `serialize:"true"`
}

func (b *ApricotCommitBlock) initialize(bytes []byte) error {
	b.CommonBlock.initialize(bytes)
	return nil
}

func (*ApricotCommitBlock) Txs() []*txs.Tx          { return nil }
func (b *ApricotCommitBlock) Visit(v Visitor) error { return v.ApricotCommitBlock(b) }

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
	return blk, initialize(blk)
}

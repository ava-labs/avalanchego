// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &AbortBlock{}
	_ Block = &CommitBlock{}
)

type AbortBlock struct {
	CommonBlock `serialize:"true"`
}

func (ab *AbortBlock) initialize(bytes []byte) error {
	ab.CommonBlock.initialize(bytes)
	return nil
}

func (ab *AbortBlock) Txs() []*txs.Tx { return nil }

func (ab *AbortBlock) Visit(v Visitor) error {
	return v.AbortBlock(ab)
}

func NewAbortBlock(
	parentID ids.ID,
	height uint64,
) (*AbortBlock, error) {
	blk := &AbortBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
	}
	return blk, initialize(blk)
}

type CommitBlock struct {
	CommonBlock `serialize:"true"`
}

func (cb *CommitBlock) initialize(bytes []byte) error {
	cb.CommonBlock.initialize(bytes)
	return nil
}

func (cb *CommitBlock) Txs() []*txs.Tx { return nil }

func (cb *CommitBlock) Visit(v Visitor) error {
	return v.CommitBlock(cb)
}

func NewCommitBlock(
	parentID ids.ID,
	height uint64,
) (*CommitBlock, error) {
	blk := &CommitBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
	}
	return blk, initialize(blk)
}

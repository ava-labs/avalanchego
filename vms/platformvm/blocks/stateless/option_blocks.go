// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &AbortBlock{}
	_ Block = &CommitBlock{}
)

func NewAbortBlock(
	version uint16,
	timestamp uint64,
	parentID ids.ID,
	height uint64,
) (Block, error) {
	res := &AbortBlock{
		CommonBlock: CommonBlock{
			PrntID:       parentID,
			Hght:         height,
			BlkTimestamp: timestamp,
		},
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(res)
	bytes, err := Codec.Marshal(version, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	return res, res.Initialize(version, bytes)
}

func (ab *AbortBlock) BlockTxs() []*txs.Tx { return nil }

func (ab *AbortBlock) Visit(v Visitor) error {
	return v.VisitAbortBlock(ab)
}

type AbortBlock struct {
	CommonBlock `serialize:"true"`
}

func NewCommitBlock(
	version uint16,
	timestamp uint64,
	parentID ids.ID,
	height uint64,
) (Block, error) {
	res := &CommitBlock{
		CommonBlock: CommonBlock{
			PrntID:       parentID,
			Hght:         height,
			BlkTimestamp: timestamp,
		},
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(res)
	bytes, err := Codec.Marshal(version, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	return res, res.Initialize(version, bytes)
}

func (cb *CommitBlock) BlockTxs() []*txs.Tx { return nil }

func (cb *CommitBlock) Visit(v Visitor) error {
	return v.VisitCommitBlock(cb)
}

type CommitBlock struct {
	CommonBlock `serialize:"true"`
}

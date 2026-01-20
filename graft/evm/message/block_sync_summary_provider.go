// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type BlockSyncSummaryProvider struct {
	codec codec.Manager
}

func NewBlockSyncSummaryProvider(c codec.Manager) *BlockSyncSummaryProvider {
	return &BlockSyncSummaryProvider{codec: c}
}

// StateSummaryAtBlock returns the block state summary at [block] if valid.
func (p *BlockSyncSummaryProvider) StateSummaryAtBlock(blk *types.Block) (block.StateSummary, error) {
	return NewBlockSyncSummary(p.codec, blk.Hash(), blk.NumberU64(), blk.Root())
}

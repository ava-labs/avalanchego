// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package message

import (
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/libevm/core/types"
)

type BlockSyncSummaryProvider struct{}

// StateSummaryAtBlock returns the block state summary at [block] if valid.
func (a *BlockSyncSummaryProvider) StateSummaryAtBlock(blk *types.Block) (block.StateSummary, error) {
	return NewBlockSyncSummary(blk.Hash(), blk.NumberU64(), blk.Root())
}

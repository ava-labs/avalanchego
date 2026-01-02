// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type BlockSyncSummaryProvider struct{}

// StateSummaryAtBlock returns the block state summary at [block] if valid.
func (*BlockSyncSummaryProvider) StateSummaryAtBlock(blk *types.Block) (block.StateSummary, error) {
	return NewBlockSyncSummary(blk.Hash(), blk.NumberU64(), blk.Root())
}

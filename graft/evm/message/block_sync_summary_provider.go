// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ SummaryProvider = (*BlockSyncSummaryProvider)(nil)
	_ SyncableParser = (*BlockSyncSummaryProvider)(nil)
)

type BlockSyncSummaryProvider struct {
	codec codec.Manager
}

func NewBlockSyncSummaryProvider(c codec.Manager) *BlockSyncSummaryProvider {
	return &BlockSyncSummaryProvider{codec: c}
}

// StateSummaryAtBlock returns the block state summary at [block] if valid.
func (c *BlockSyncSummaryProvider) StateSummaryAtBlock(blk *types.Block) (block.StateSummary, error) {
	return NewBlockSyncSummary(c.codec, blk.Hash(), blk.NumberU64(), blk.Root())
}

func (c *BlockSyncSummaryProvider) Parse(summaryBytes []byte, acceptImpl AcceptImplFn) (Syncable, error) {
	summary := BlockSyncSummary{}
	summaryID, err := ParseSyncableSummary(c.codec, summaryBytes, &summary)
	if err != nil {
		return nil, err
	}

	summary.bytes = summaryBytes
	summary.summaryID = summaryID
	summary.acceptImpl = acceptImpl
	return &summary, nil
}

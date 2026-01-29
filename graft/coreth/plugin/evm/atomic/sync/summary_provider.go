// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

var _ message.SyncSummaryProvider = (*SummaryProvider)(nil)

// SummaryProvider provides and parses state sync summaries for the atomic trie.
type SummaryProvider struct {
	trie *state.AtomicTrie
}

// Initialize initializes the summary provider with the atomic trie.
func (a *SummaryProvider) Initialize(trie *state.AtomicTrie) {
	a.trie = trie
}

// StateSummaryAtBlock returns the block state summary at [blk] if valid.
func (a *SummaryProvider) StateSummaryAtBlock(blk *ethtypes.Block) (block.StateSummary, error) {
	height := blk.NumberU64()
	atomicRoot, err := a.trie.Root(height)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve atomic trie root for height (%d): %w", height, err)
	}

	if atomicRoot == (common.Hash{}) {
		return nil, fmt.Errorf("atomic trie root not found for height (%d)", height)
	}

	summary, err := NewSummary(blk.Hash(), height, blk.Root(), atomicRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to construct syncable block at height %d: %w", height, err)
	}
	return summary, nil
}

// Parse parses the summary bytes into a Syncable summary.
func (*SummaryProvider) Parse(summaryBytes []byte, acceptImpl message.AcceptImplFn) (message.Syncable, error) {
	summary := Summary{}
	summaryID, err := message.ParseSyncableSummary(message.CorethCodec, summaryBytes, &summary)
	if err != nil {
		return nil, err
	}

	summary.bytes = summaryBytes
	summary.summaryID = summaryID
	summary.acceptImpl = acceptImpl
	return &summary, nil
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var errProviderNotSet = errors.New("provider not set")

// BlockChain provides the blockchain interface needed by the state sync server.
// This interface abstracts the blockchain operations required for serving state summaries.
type BlockChain interface {
	// LastAcceptedBlock returns the last accepted block.
	LastAcceptedBlock() *types.Block

	// GetBlockByNumber returns the block at the given height, or nil if not found.
	GetBlockByNumber(number uint64) *types.Block

	// HasState returns true if the state for the given root hash is available.
	HasState(root common.Hash) bool

	// ResetToStateSyncedBlock resets the blockchain to the given synced block.
	ResetToStateSyncedBlock(block *types.Block) error

	// TrieDB returns the database used for storing the state trie.
	TrieDB() *triedb.Database
}

// SummaryProvider provides state summaries for blocks.
type SummaryProvider interface {
	StateSummaryAtBlock(ethBlock *types.Block) (block.StateSummary, error)
}

type server struct {
	chain BlockChain

	provider         SummaryProvider
	syncableInterval uint64
}

type Server interface {
	GetLastStateSummary(context.Context) (block.StateSummary, error)
	GetStateSummary(context.Context, uint64) (block.StateSummary, error)
}

func NewServer(chain BlockChain, provider SummaryProvider, syncableInterval uint64) Server {
	return &server{
		chain:            chain,
		syncableInterval: syncableInterval,
		provider:         provider,
	}
}

// GetLastStateSummary returns the latest state summary.
// State summary is calculated by the block nearest to last accepted
// that is divisible by [syncableInterval]
// If no summary is available, [database.ErrNotFound] must be returned.
func (s *server) GetLastStateSummary(context.Context) (block.StateSummary, error) {
	lastHeight := s.chain.LastAcceptedBlock().NumberU64()
	lastSyncSummaryNumber := lastHeight - lastHeight%s.syncableInterval

	summary, err := s.stateSummaryAtHeight(lastSyncSummaryNumber)
	if err != nil {
		log.Debug("could not get latest state summary", "err", err)
		return nil, database.ErrNotFound
	}
	log.Debug("Serving syncable block at latest height", "summary", summary)
	return summary, nil
}

// GetStateSummary implements StateSyncableVM and returns a summary corresponding
// to the provided [height] if the node can serve state sync data for that key.
// If not, [database.ErrNotFound] must be returned.
func (s *server) GetStateSummary(_ context.Context, height uint64) (block.StateSummary, error) {
	summaryBlock := s.chain.GetBlockByNumber(height)
	if summaryBlock == nil ||
		summaryBlock.NumberU64() > s.chain.LastAcceptedBlock().NumberU64() ||
		summaryBlock.NumberU64()%s.syncableInterval != 0 {
		return nil, database.ErrNotFound
	}

	summary, err := s.stateSummaryAtHeight(summaryBlock.NumberU64())
	if err != nil {
		log.Debug("could not get state summary", "height", height, "err", err)
		return nil, database.ErrNotFound
	}

	log.Debug("Serving syncable block at requested height", "height", height, "summary", summary)
	return summary, nil
}

func (s *server) stateSummaryAtHeight(height uint64) (block.StateSummary, error) {
	blk := s.chain.GetBlockByNumber(height)
	if blk == nil {
		return nil, fmt.Errorf("block not found for height (%d)", height)
	}

	if !s.chain.HasState(blk.Root()) {
		return nil, fmt.Errorf("block root does not exist for height (%d), root (%s)", height, blk.Root())
	}
	if s.provider == nil {
		return nil, errProviderNotSet
	}
	return s.provider.StateSummaryAtBlock(blk)
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
)

type stateSyncServerConfig struct {
	Chain *core.BlockChain

	// SyncableInterval is the interval at which blocks are eligible to provide syncable block summaries.
	SyncableInterval uint64
}

type stateSyncServer struct {
	chain *core.BlockChain

	syncableInterval uint64
}

type StateSyncServer interface {
	GetLastStateSummary(context.Context) (block.StateSummary, error)
	GetStateSummary(context.Context, uint64) (block.StateSummary, error)
}

func NewStateSyncServer(config *stateSyncServerConfig) StateSyncServer {
	return &stateSyncServer{
		chain:            config.Chain,
		syncableInterval: config.SyncableInterval,
	}
}

// stateSummaryAtHeight returns the SyncSummary at [height] if valid and available.
func (server *stateSyncServer) stateSummaryAtHeight(height uint64) (message.SyncSummary, error) {
	blk := server.chain.GetBlockByNumber(height)
	if blk == nil {
		return message.SyncSummary{}, fmt.Errorf("block not found for height (%d)", height)
	}

	if !server.chain.HasState(blk.Root()) {
		return message.SyncSummary{}, fmt.Errorf("block root does not exist for height (%d), root (%s)", height, blk.Root())
	}

	summary, err := message.NewSyncSummary(blk.Hash(), height, blk.Root())
	if err != nil {
		return message.SyncSummary{}, fmt.Errorf("failed to construct syncable block at height %d: %w", height, err)
	}
	return summary, nil
}

// GetLastStateSummary returns the latest state summary.
// State summary is calculated by the block nearest to last accepted
// that is divisible by [syncableInterval]
// If no summary is available, [database.ErrNotFound] must be returned.
func (server *stateSyncServer) GetLastStateSummary(context.Context) (block.StateSummary, error) {
	lastHeight := server.chain.LastAcceptedBlock().NumberU64()
	lastSyncSummaryNumber := lastHeight - lastHeight%server.syncableInterval

	summary, err := server.stateSummaryAtHeight(lastSyncSummaryNumber)
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
func (server *stateSyncServer) GetStateSummary(_ context.Context, height uint64) (block.StateSummary, error) {
	summaryBlock := server.chain.GetBlockByNumber(height)
	if summaryBlock == nil ||
		summaryBlock.NumberU64() > server.chain.LastAcceptedBlock().NumberU64() ||
		summaryBlock.NumberU64()%server.syncableInterval != 0 {
		return nil, database.ErrNotFound
	}

	summary, err := server.stateSummaryAtHeight(summaryBlock.NumberU64())
	if err != nil {
		log.Debug("could not get state summary", "height", height, "err", err)
		return nil, database.ErrNotFound
	}

	log.Debug("Serving syncable block at requested height", "height", height, "summary", summary)
	return summary, nil
}

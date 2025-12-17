// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var errProviderNotSet = errors.New("provider not set")

type SummaryProvider interface {
	StateSummaryAtBlock(ethBlock *types.Block) (block.StateSummary, error)
}

type server struct {
	chain *core.BlockChain

	provider         SummaryProvider
	syncableInterval uint64
}

type Server interface {
	GetLastStateSummary(context.Context) (block.StateSummary, error)
	GetStateSummary(context.Context, uint64) (block.StateSummary, error)
}

func NewServer(chain *core.BlockChain, provider SummaryProvider, syncableInterval uint64) Server {
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
func (server *server) GetLastStateSummary(context.Context) (block.StateSummary, error) {
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
func (server *server) GetStateSummary(_ context.Context, height uint64) (block.StateSummary, error) {
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

func (server *server) stateSummaryAtHeight(height uint64) (block.StateSummary, error) {
	blk := server.chain.GetBlockByNumber(height)
	if blk == nil {
		return nil, fmt.Errorf("block not found for height (%d)", height)
	}

	if !server.chain.HasState(blk.Root()) {
		return nil, fmt.Errorf("block root does not exist for height (%d), root (%s)", height, blk.Root())
	}
	if server.provider == nil {
		return nil, errProviderNotSet
	}
	return server.provider.StateSummaryAtBlock(blk)
}

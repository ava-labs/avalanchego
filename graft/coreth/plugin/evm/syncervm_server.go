// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/plugin/evm/message"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

type stateSyncServerConfig struct {
	Chain      *core.BlockChain
	AtomicTrie AtomicTrie

	// SyncableInterval is the interval at which blocks are eligible to provide syncable block summaries.
	SyncableInterval uint64
}

type stateSyncServer struct {
	chain      *core.BlockChain
	atomicTrie AtomicTrie

	syncableInterval uint64
}

type StateSyncServer interface {
	GetLastStateSummary() (block.StateSummary, error)
	GetStateSummary(uint64) (block.StateSummary, error)
}

func NewStateSyncServer(config *stateSyncServerConfig) StateSyncServer {
	return &stateSyncServer{
		chain:            config.Chain,
		atomicTrie:       config.AtomicTrie,
		syncableInterval: config.SyncableInterval,
	}
}

// stateSummaryAtHeight returns the SyncSummary at [height] if valid and available.
func (server *stateSyncServer) stateSummaryAtHeight(height uint64) (message.SyncSummary, error) {
	atomicRoot, err := server.atomicTrie.Root(height)
	if err != nil {
		return message.SyncSummary{}, fmt.Errorf("error getting atomic trie root for height (%d): %w", height, err)
	}

	if (atomicRoot == common.Hash{}) {
		return message.SyncSummary{}, fmt.Errorf("atomic trie root not found for height (%d)", height)
	}

	blk := server.chain.GetBlockByNumber(height)
	if blk == nil {
		return message.SyncSummary{}, fmt.Errorf("block not found for height (%d)", height)
	}

	if !server.chain.HasState(blk.Root()) {
		return message.SyncSummary{}, fmt.Errorf("block root does not exist for height (%d), root (%s)", height, blk.Root())
	}

	summary, err := message.NewSyncSummary(blk.Hash(), height, blk.Root(), atomicRoot)
	if err != nil {
		return message.SyncSummary{}, fmt.Errorf("failed to construct syncable block at height %d: %w", height, err)
	}
	return summary, nil
}

// GetLastStateSummary returns the latest state summary.
// State summary is calculated by the block nearest to last accepted
// that is divisible by [syncableInterval]
// If no summary is available, [database.ErrNotFound] must be returned.
func (server *stateSyncServer) GetLastStateSummary() (block.StateSummary, error) {
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
func (server *stateSyncServer) GetStateSummary(height uint64) (block.StateSummary, error) {
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

// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saexec provides the execution module of [Streaming Asynchronous
// Execution] (SAE).
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
package saexec

import (
	"fmt"
	"io"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/libevm/eventual"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/saedb"
)

// SnapshotCacheSizeMB is the snapshot cache size used by the executor.
const SnapshotCacheSizeMB = 128

// An Executor accepts and executes a [blocks.Block] FIFO queue.
type Executor struct {
	quit, done chan struct{}
	log        logging.Logger
	hooks      hook.Points

	queue        chan *blocks.Block
	lastExecuted atomic.Pointer[blocks.Block]

	headEvents  event.FeedOf[core.ChainHeadEvent]
	chainEvents event.FeedOf[core.ChainEvent]
	logEvents   event.FeedOf[[]*types.Log]
	receipts    *syncMap[common.Hash, eventual.Value[*Receipt]]

	chainContext *chainContext
	chainConfig  *params.ChainConfig
	db           ethdb.Database
	xdb          saedb.ExecutionResults
	stateCache   state.Database
	// snaps is owned by [Executor]. It may be mutated during
	// [Executor.execute] and [Executor.Close]. Callers MUST treat
	// values returned from [Executor.SnapshotTree] as read-only.
	//
	// [snapshot.Tree] is safe for concurrent read access - for example,
	// blockchain_reader.go exposes bc.snaps without holding any lock:
	// https://github.com/ava-labs/libevm/blob/312fa380513e/core/blockchain_reader.go#L356-L367
	snaps *snapshot.Tree
}

// New constructs and starts a new [Executor]. Call [Executor.Close] to release
// resources created by this constructor.
//
// The last-executed block MAY be the genesis block for an always-SAE chain, the
// last pre-SAE synchronous block during transition, or the last asynchronously
// executed block after shutdown and recovery.
func New(
	lastExecuted *blocks.Block,
	headerSrc blocks.HeaderSource,
	chainConfig *params.ChainConfig,
	db ethdb.Database,
	xdb saedb.ExecutionResults,
	triedbConfig *triedb.Config,
	hooks hook.Points,
	log logging.Logger,
) (*Executor, error) {
	cache := state.NewDatabaseWithConfig(db, triedbConfig)
	snapConf := snapshot.Config{
		CacheSize:  SnapshotCacheSizeMB,
		AsyncBuild: true,
	}
	snaps, err := snapshot.New(snapConf, db, cache.TrieDB(), lastExecuted.PostExecutionStateRoot())
	if err != nil {
		return nil, err
	}

	e := &Executor{
		quit:  make(chan struct{}), // closed by [Executor.Close]
		done:  make(chan struct{}), // closed by [Executor.processQueue] after `quit` is closed
		log:   log,
		hooks: hooks,
		// On startup we enqueue every block since the last time the trie DB was
		// committed, so the queue needs sufficient capacity to avoid
		// [Executor.Enqueue] warning about it being too full.
		queue: make(chan *blocks.Block, 2*saedb.CommitTrieDBEvery),
		chainContext: &chainContext{
			headerSrc,
			lru.NewCache[uint64, *types.Header](256), // minimum history for BLOCKHASH op
			log,
		},
		chainConfig: chainConfig,
		db:          db,
		stateCache:  cache,
		snaps:       snaps,
		xdb:         xdb,
		receipts:    newSyncMap[common.Hash, eventual.Value[*Receipt]](),
	}
	e.lastExecuted.Store(lastExecuted)

	go e.processQueue()
	return e, nil
}

var _ io.Closer = (*Executor)(nil)

// Close shuts down the [Executor], waits for the currently executing block
// to complete, and then releases all resources.
func (e *Executor) Close() error {
	close(e.quit)
	<-e.done

	// We don't use [snapshot.Tree.Journal] because re-orgs are impossible under
	// SAE so we don't mind flattening all snapshot layers to disk. Note that
	// calling `Cap([disk root], 0)` returns an error when it's actually a
	// no-op, so we ignore it.
	if root := e.LastExecuted().PostExecutionStateRoot(); root != e.snaps.DiskRoot() {
		if err := e.snaps.Cap(root, 0); err != nil {
			return fmt.Errorf("snapshot.Tree.Cap([last post-execution state root], 0): %v", err)
		}
	}

	e.snaps.Release()
	return nil
}

// SignerForBlock returns the transaction signer for the block.
func (e *Executor) SignerForBlock(b *types.Block) types.Signer {
	return types.MakeSigner(e.chainConfig, b.Number(), b.Time())
}

// ChainConfig returns the config originally passed to [New].
func (e *Executor) ChainConfig() *params.ChainConfig {
	return e.chainConfig
}

// ChainContext returns a context backed by the [blocks.Source] originally
// passed to [New].
func (e *Executor) ChainContext() core.ChainContext {
	return e.chainContext
}

// StateCache returns caching database underpinning execution.
func (e *Executor) StateCache() state.Database {
	return e.stateCache
}

// SnapshotTree returns the snapshot tree, which MUST only be used for reading.
func (e *Executor) SnapshotTree() *snapshot.Tree {
	return e.snaps
}

// LastExecuted returns the last-executed block in a threadsafe manner.
func (e *Executor) LastExecuted() *blocks.Block {
	return e.lastExecuted.Load()
}

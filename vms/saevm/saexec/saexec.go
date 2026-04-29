// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saexec provides the execution module of [Streaming Asynchronous
// Execution] (SAE).
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
package saexec

import (
	"io"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/libevm/eventual"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

var _ saedb.StateDBOpener = (*Executor)(nil)

// An Executor accepts and executes a [blocks.Block] FIFO queue.
type Executor struct {
	*saedb.Tracker
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
	xdb          saetypes.ExecutionResults
}

// New constructs and starts a new [Executor]. Call [Executor.Close] to release
// resources created by this constructor.
//
// The last-executed block MAY be the genesis block for an always-SAE chain, the
// last pre-SAE synchronous block during transition, or the last asynchronously
// executed block after shutdown and recovery.
func New(
	lastExecuted *blocks.Block,
	headerSrc saetypes.HeaderSource,
	chainConfig *params.ChainConfig,
	db ethdb.Database,
	xdb saetypes.ExecutionResults,
	saedbConfig saedb.Config,
	hooks hook.Points,
	log logging.Logger,
) (*Executor, error) {
	t, err := saedb.NewTracker(db, saedbConfig, lastExecuted.PostExecutionStateRoot(), log)
	if err != nil {
		return nil, err
	}

	e := &Executor{
		Tracker: t,
		quit:    make(chan struct{}), // closed by [Executor.Close]
		done:    make(chan struct{}), // closed by [Executor.processQueue] after `quit` is closed
		log:     log,
		hooks:   hooks,
		// On startup we enqueue every block since the last time the trie DB was
		// committed, so the queue needs sufficient capacity to avoid
		// [Executor.Enqueue] warning about it being too full.
		queue: make(chan *blocks.Block, 2*saedbConfig.CommitInterval()),
		chainContext: &chainContext{
			headerSrc,
			lru.NewCache[uint64, *types.Header](256), // minimum history for BLOCKHASH op
			log,
		},
		chainConfig: chainConfig,
		db:          db,
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

	return e.Tracker.Close(e.LastExecuted().PostExecutionStateRoot())
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

// LastExecuted returns the last-executed block in a threadsafe manner.
func (e *Executor) LastExecuted() *blocks.Block {
	return e.lastExecuted.Load()
}

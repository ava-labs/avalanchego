// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saexec provides the execution module of [Streaming Asynchronous
// Execution] (SAE).
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
package saexec

import (
	"fmt"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/libevm/eventual"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"

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
	done  chan struct{}
	log   logging.Logger
	hooks hook.Points

	queue        chan queuedBlock
	lastExecuted atomic.Pointer[blocks.Block]

	headEvents  event.FeedOf[core.ChainHeadEvent]
	chainEvents event.FeedOf[core.ChainEvent]
	logEvents   event.FeedOf[[]*types.Log]
	receipts    *syncMap[common.Hash, eventual.Value[*Receipt]]

	chainContext *chainContext
	chainConfig  *params.ChainConfig
	db           ethdb.Database
	xdb          saetypes.ExecutionResults
	metrics      *metrics
}

// New constructs and starts a new [Executor]. Call [Executor.Close] to stop it.
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
	tracker *saedb.Tracker,
	hooks hook.Points,
	logger logging.Logger,
	reg prometheus.Registerer,
) (*Executor, error) {
	m, err := newMetrics(reg, lastExecuted)
	if err != nil {
		return nil, fmt.Errorf("initializing saexec metrics: %w", err)
	}

	e := &Executor{
		Tracker: tracker,
		done:    make(chan struct{}), // closed by [Executor.processQueue] once `queue` is closed and drained
		log:     logger,
		hooks:   hooks,
		// On startup we enqueue every block since the last time the trie DB was
		// committed, so the queue needs sufficient capacity to avoid
		// [Executor.Enqueue] warning about it being too full.
		// queue is closed by [Executor.Close].
		queue: make(chan queuedBlock, 2*tracker.CommitInterval()),
		chainContext: &chainContext{
			headerSrc,
			lru.NewCache[uint64, *types.Header](256), // minimum history for BLOCKHASH op
			logger,
		},
		chainConfig: chainConfig,
		db:          db,
		xdb:         xdb,
		metrics:     m,
		receipts:    newSyncMap[common.Hash, eventual.Value[*Receipt]](),
	}
	e.lastExecuted.Store(lastExecuted)

	go e.processQueue()
	return e, nil
}

// Close shuts down the [Executor] and waits for all queued blocks to finish
// executing.
func (e *Executor) Close() {
	close(e.queue)
	<-e.done
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

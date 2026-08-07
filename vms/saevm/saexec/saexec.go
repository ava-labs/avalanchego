// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saexec provides the execution module of [Streaming Asynchronous
// Execution] (SAE).
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
package saexec

import (
	"errors"
	"fmt"
	"io"
	"math"
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
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

var (
	_ saedb.StateDBOpener = (*Executor)(nil)

	ErrZeroStateReplayConcurrency     = errors.New("state replay concurrency must be non-zero")
	ErrStateReplayConcurrencyTooLarge = errors.New("state replay concurrency exceeds max")
)

// DefaultStateReplayConcurrency is the maximum number of historical state
// requests that may replay blocks concurrently by default.
const DefaultStateReplayConcurrency uint64 = 1

// Config controls historical state replay resource usage.
type Config struct {
	StateReplayConcurrency uint64
}

// DefaultConfig returns the [Config] used when an operator configures nothing.
func DefaultConfig() Config {
	return Config{
		StateReplayConcurrency: DefaultStateReplayConcurrency,
	}
}

// Verify checks that the configuration can execute historical state replay.
func (c Config) Verify() error {
	if c.StateReplayConcurrency == 0 {
		return ErrZeroStateReplayConcurrency
	}
	if c.StateReplayConcurrency > math.MaxInt {
		return fmt.Errorf("%w: %d > %d", ErrStateReplayConcurrencyTooLarge, c.StateReplayConcurrency, math.MaxInt)
	}
	return nil
}

// An Executor accepts and executes a [blocks.Block] FIFO queue.
type Executor struct {
	*saedb.Tracker
	quit, done chan struct{}
	log        logging.Logger
	hooks      hook.Points

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

	// commitInterval is the minimum distance [Executor.StateAt] searches for a
	// state to reconstruct from. The caller may request a larger replay horizon.
	commitInterval uint64

	// replaySlots bounds concurrent block replay and final root calculation for
	// historical state requests. Exact reconstructed states do not use a slot.
	replaySlots chan struct{}
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
	config Config,
	hooks hook.Points,
	snowCtx *snow.Context,
	reg prometheus.Registerer,
) (*Executor, error) {
	if err := config.Verify(); err != nil {
		return nil, err
	}

	t, err := saedb.NewTracker(db, saedbConfig, lastExecuted.PostExecutionStateRoot(), snowCtx.ChainDataDir, snowCtx.Log)
	if err != nil {
		return nil, err
	}

	m, err := newMetrics(reg, lastExecuted)
	if err != nil {
		return nil, fmt.Errorf("initializing saexec metrics: %w", err)
	}

	e := &Executor{
		Tracker: t,
		quit:    make(chan struct{}), // closed by [Executor.Close]
		done:    make(chan struct{}), // closed by [Executor.processQueue] after `quit` is closed
		log:     snowCtx.Log,
		hooks:   hooks,
		// On startup we enqueue every block since the last time the trie DB was
		// committed, so the queue needs sufficient capacity to avoid
		// [Executor.Enqueue] warning about it being too full.
		queue: make(chan queuedBlock, 2*saedbConfig.CommitInterval),
		chainContext: &chainContext{
			headerSrc,
			lru.NewCache[uint64, *types.Header](256), // minimum history for BLOCKHASH op
			snowCtx.Log,
		},
		chainConfig:    chainConfig,
		db:             db,
		xdb:            xdb,
		metrics:        m,
		receipts:       newSyncMap[common.Hash, eventual.Value[*Receipt]](),
		commitInterval: saedbConfig.CommitInterval,
		replaySlots:    make(chan struct{}, config.StateReplayConcurrency),
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

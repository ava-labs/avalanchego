// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/unwind"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

type recovery struct {
	db          ethdb.Database
	xdb         saetypes.ExecutionResults
	chainConfig *params.ChainConfig
	snowCtx     *snow.Context
	hooks       hook.Points
	config      Config
}

func (rec *recovery) newCanonicalBlock(num uint64, parent *blocks.Block) (*blocks.Block, error) {
	ethB, err := canonicalBlock(rec.db, num)
	if err != nil {
		return nil, err
	}
	return blocks.New(ethB, parent, nil, rec.hooks, rec.snowCtx.Log)
}

// lastCommittedBlock returns the highest settled block whose post-execution
// state is available on disk. This is required because its post-execution state
// is the basis for the worst-case checks needed for block verifications.
func (rec *recovery) lastCommittedBlock() (_ *blocks.Block, retErr error) {
	cache := state.NewDatabaseWithConfig(rec.db, rec.config.DBConfig.TrieDBConfig(rec.snowCtx.ChainDataDir, rec.snowCtx.Log))
	defer func() {
		// Unlike elsewhere in this package, the trie database MUST be closed on
		// both the success and error paths; it is only used to probe for
		// available state and ownership is never transferred to the caller.
		retErr = errors.Join(retErr, cache.TrieDB().Close())
	}()

	lastSettledHash := rawdb.ReadFinalizedBlockHash(rec.db)
	if lastSettledHash == (common.Hash{}) {
		return nil, errors.New("no finalized block recorded")
	}
	lastSettledHeight := rawdb.ReadHeaderNumber(rec.db, lastSettledHash)
	if lastSettledHeight == nil {
		return nil, fmt.Errorf("no height for finalized block %s", lastSettledHash)
	}

	rec.snowCtx.Log.Info(
		"searching for state from last settled block",
		zap.Stringer("hash", lastSettledHash),
		zap.Uint64("height", *lastSettledHeight),
	)

	// Search for highest settled post-execution state
	// Invariant: The state is written to disk AFTER the block is written to
	// disk. Therefore, the state can only lag behind the block read.
	// Additionally, we assume any block has been written atomically, so
	// if the last settled height was found, the underlying block is present.
	// At minimum, [NewVM] requires a genesis block to be written (which is
	// synchronous by definition).
	//
	// There's no reasonable cap on how far back to search, since the distance
	// between the settler and settled block is unbounded, and node crashes
	// must be accounted for.
	for height := *lastSettledHeight; ; height-- {
		ethB, err := canonicalBlock(rec.db, height)
		if err != nil {
			return nil, err
		}

		b, err := blocks.RestoreSettledBlock(ethB, rec.hooks, rec.snowCtx.Log, rec.db, rec.xdb, rec.chainConfig)
		if err != nil {
			return nil, err
		}

		if _, err := state.New(b.PostExecutionStateRoot(), cache, nil); err == nil { // if NO error
			rec.snowCtx.Log.Info(
				"found most recently executed settled block with available post-execution state",
				zap.Stringer("hash", b.Hash()),
				zap.Uint64("height", height),
			)
			return b, nil
		}

		if b.Synchronous() {
			return nil, fmt.Errorf("last synchronous block %d has no available post-execution state", height)
		}
	}
}

// recoverExecutor returns an [saexec.Executor] that is ready to execute any
// child of the last-known accepted block, and a map of all consensus-critical
// blocks.
//
// The [saedb.Tracker] contained within the [saexec.Executor] MUST be closed
// after the executor.
func recoverExecutor(
	ctx context.Context,
	db ethdb.Database,
	xdb saetypes.ExecutionResults,
	chainConfig *params.ChainConfig,
	snowCtx *snow.Context,
	hooks hook.Points,
	cfg Config,
	reg prometheus.Registerer,
) (
	_ *saexec.Executor,
	_ *syncMap[common.Hash, *blocks.Block],
	retErr error,
) {
	var closers unwind.Closers
	defer closers.CloseIfPointsToNonNil(&retErr)

	rec := &recovery{db, xdb, chainConfig, snowCtx, hooks, cfg}

	lastCommitted, err := rec.lastCommittedBlock()
	if err != nil {
		return nil, nil, fmt.Errorf("finding last committed state: %w", err)
	}
	lastCommittedRoot := lastCommitted.PostExecutionStateRoot()

	tracker, err := saedb.NewTracker(
		rec.db,
		rec.config.DBConfig,
		lastCommittedRoot,
		rec.snowCtx.ChainDataDir,
		rec.snowCtx.Log,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("saedb.NewTracker(...): %w", err)
	}
	closers.Push(unwind.CloserFuncT(tracker.Close, lastCommittedRoot))

	consensusCritical := newSyncMap[common.Hash, *blocks.Block](
		func(b *blocks.Block) {
			tracker.Track(b.SettledStateRoot())
			// The post-execution root is tracked by the [saexec.Executor]
			// as soon as it's known. In the case of database recovery,
			// this occurred in [recovery.executeAllAccepted].
		},
		func(b *blocks.Block) {
			tracker.Untrack(b.SettledStateRoot())
			if b.Executed() { // i.e. deleted due to settlement not rejection
				tracker.Untrack(b.PostExecutionStateRoot())
			}
		},
	)

	exec, err := saexec.New(
		lastCommitted,
		headerSource(consensusCritical, rec.db),
		rec.chainConfig,
		rec.db,
		rec.xdb,
		tracker,
		rec.hooks,
		rec.snowCtx.Log,
		reg,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("saexec.New(...): %v", err)
	}
	closers.Push(exec)

	if err := rec.executeAllAccepted(ctx, exec); err != nil {
		return nil, nil, fmt.Errorf("executing all previously accepted blocks: %w", err)
	}
	if err := rec.populateConsensusCriticalBlocks(exec, consensusCritical); err != nil {
		return nil, nil, fmt.Errorf("finding consensus-critical blocks: %w", err)
	}
	return exec, consensusCritical, nil
}

func (rec *recovery) canonicalAfter(parent *blocks.Block) iter.Seq2[*blocks.Block, error] {
	return func(yield func(*blocks.Block, error) bool) {
		lastAcceptedHash := rawdb.ReadHeadFastBlockHash(rec.db)
		rec.snowCtx.Log.Info(
			"finding canonical blocks",
			zap.Stringer("parent_hash", parent.Hash()),
			zap.Uint64("parent_height", parent.Height()),
			zap.Stringer("last_accepted_hash", lastAcceptedHash),
		)

		if lastAcceptedHash == (common.Hash{}) {
			// SAE writes this hash on [VM.AcceptBlock], so the set of accepted,
			// asynchronous blocks MUST be empty.
			return
		}

		for curr := parent; curr.Hash() != lastAcceptedHash; {
			b, err := rec.newCanonicalBlock(curr.Height()+1, curr)
			if !yield(b, err) || err != nil {
				return
			}
			curr = b
		}
	}
}

func (rec *recovery) executeAllAccepted(ctx context.Context, exec *saexec.Executor) error {
	after := exec.LastExecuted()
	last := after
	for b, err := range rec.canonicalAfter(after) {
		if err != nil {
			return err
		}
		if err := exec.Enqueue(ctx, b); err != nil {
			return err
		}
		last = b
	}
	if err := last.WaitUntilExecuted(ctx); err != nil {
		return err
	}

	rec.snowCtx.Log.Info(
		"executed all accepted blocks",
		zap.Uint64("previously_executed_height", after.Height()),
		zap.Uint64("last_accepted_height", last.Height()),
	)

	// Consensus only requires post-execution state after and including the
	// last-settled block.
	keepFrom := rec.hooks.SettledBy(last.Header()).Height
	for b := last; b.NumberU64() > after.NumberU64(); b = b.ParentBlock() {
		if b.NumberU64() < keepFrom {
			exec.Tracker.Untrack(b.PostExecutionStateRoot())
		}
	}
	return nil
}

// lastOf returns the lastOf element in a slice, which MUST NOT be empty.
func lastOf[E any](s []E) E {
	return s[len(s)-1]
}

// populateConsensusCriticalBlocks populates bMap with all blocks from the last
// executed back to, and including, the block that it settled. bMap MUST be
// empty and MUST already have its callbacks bound to the executor's state
// tracker.
func (rec *recovery) populateConsensusCriticalBlocks(exec *saexec.Executor, bMap *syncMap[common.Hash, *blocks.Block]) error {
	chain := []*blocks.Block{exec.LastExecuted()} // reverse height order

	// extend appends to the chain all the blocks in settler's ancestry up to
	// and including the block that it settled.
	extend := func(settler *blocks.Block) error {
		end := rec.hooks.SettledBy(settler.Header()).Height
		for b := lastOf(chain); b.Height() > end && !b.Synchronous(); b = lastOf(chain) {
			parent, err := rec.newCanonicalBlock(b.Height()-1, nil)
			if err != nil {
				return err
			}
			chain = append(chain, parent)
		}
		return nil
	}

	if err := extend(exec.LastExecuted()); err != nil {
		return err
	}
	var (
		critical    = slices.Clone(chain)
		lastSettled = lastOf(chain)
		unsettled   = chain[:len(chain)-1]
	)

	// [recovery.executeAllAccepted] discarded the blocks we've just rebuilt,
	// but execution artefacts are required for determining worst-case state.
	for _, b := range critical[1:] { // [0] is [saexec.Executor.LastExecuted]
		if err := b.RestoreExecutionArtefacts(rec.db, rec.xdb, rec.chainConfig); err != nil {
			return err
		}
	}

	for i, b := range unsettled {
		if err := extend(b); err != nil {
			return err
		}
		if err := b.SetAncestors(chain[i+1], lastOf(chain)); err != nil {
			return err
		}
	}

	var (
		settled   = chain[len(unsettled):]
		blackhole = new(atomic.Pointer[blocks.Block])
	)
	for _, b := range settled {
		if b.Settled() { // e.g. genesis
			continue
		}
		if err := b.MarkSettled(blackhole); err != nil {
			return err
		}
	}

	for _, b := range critical {
		bMap.Store(b.Hash(), b)

		stage := blocks.Executed
		if b.Hash() == lastSettled.Hash() {
			stage = blocks.Settled
		}
		if err := b.CheckInvariants(stage); err != nil {
			return err
		}
	}
	return nil
}

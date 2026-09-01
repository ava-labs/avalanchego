// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/graft/evm/sync/evmstate"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"

	evmsnapshot "github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	synctypes "github.com/ava-labs/avalanchego/graft/evm/sync/types"
	syncblock "github.com/ava-labs/avalanchego/vms/evm/sync/block"
	ethcommon "github.com/ava-labs/libevm/common"
)

// StateSyncEnabled checks whether the node should query for state summaries.
func (h *SummaryHandler) StateSyncEnabled(context.Context) (bool, error) {
	return h.cfg.Enabled, nil
}

// AcceptSummary performs the entire state sync given the provided summary. If
// [SummaryHandler.StateSyncEnabled] returns false, this method should not be
// called. If this method returns [block.StateSyncSkipped], no state changes
// were made. Once the state sync is complete, [SummaryHandler.WaitForEvent]
// will return [common.StateSyncDone].
//
// AcceptSummary MUST only be called once.
func (h *SummaryHandler) AcceptSummary(ctx context.Context, s *Summary) (block.StateSyncMode, error) {
	should, err := h.ShouldAcceptSummary(s)
	if err != nil || !should {
		return block.StateSyncSkipped, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return block.StateSyncSkipped, nil
	}

	ctx, h.cancel = context.WithCancel(context.Background())
	go func() {
		defer h.cancel()
		defer close(h.done)

		// Failures are logged at Info: the error is surfaced to the engine
		// via [SummaryHandler.Error], which treats it as fatal.
		if err := h.StateSync(ctx, s); err != nil {
			h.snowCtx.Log.Info("state sync failed", zap.Error(err))
			h.err.Set(fmt.Errorf("state sync failed: %w", err))
			return
		}
		if err := h.OnFinish(s); err != nil {
			h.snowCtx.Log.Info("state sync finalization failed", zap.Error(err))
			h.err.Set(err)
			return
		}
		h.snowCtx.Log.Info("state sync finished")
	}()

	return block.StateSyncStatic, nil
}

// WaitForEvent blocks until the state sync is complete, or the context is
// canceled. Once the state sync is done, [common.StateSyncDone] is returned.
func (h *SummaryHandler) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-h.done:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

// ShouldAcceptSummary reports whether the summary should be state synced to,
// given the current disk state. Declining a summary is recorded for
// [SummaryHandler.SyncProgressOf], so that a health check can distinguish a
// chain that fell back to bootstrapping from one that has yet to be offered a
// summary.
func (h *SummaryHandler) ShouldAcceptSummary(s *Summary) (bool, error) {
	should, err := h.shouldAcceptSummary(s)
	if err == nil && !should {
		h.skipped.Set(true)
	}
	return should, err
}

// TODO(alarso16): Find a way to wire through Firewood.
func (h *SummaryHandler) shouldAcceptSummary(s *Summary) (bool, error) {
	if h.cfg.DBConfig.Scheme == customrawdb.FirewoodScheme {
		h.snowCtx.Log.Warn("State sync is not supported with Firewood scheme")
		return false, nil
	}

	// Sync iff the summary is strictly ahead of the local accepted height.
	// There is deliberately no minimum-distance threshold: after an eager
	// transition (see vms/transitionvm), local blocks below the summary may
	// belong to the pre-transition chain, which this VM cannot execute, so
	// declining a nearby summary is never the safe choice.
	hash, err := h.lastAcceptedHash()
	if err != nil {
		return false, err
	}
	var localHeight uint64
	if height := rawdb.ReadHeaderNumber(h.db, hash); height != nil {
		localHeight = *height
	}
	if s.AcceptedHeight <= localHeight {
		h.snowCtx.Log.Info("declining state sync summary at or below local height",
			zap.Uint64("summaryHeight", s.AcceptedHeight),
			zap.Stringer("summaryHash", s.AcceptedHash),
			zap.Uint64("localHeight", localHeight),
		)
		return false, nil
	}
	return true, nil
}

// Error returns an error surfaced in [SummaryHandler.AcceptSummary]. To ensure
// the state sync has finished (in success or failure), one must call
// [SummaryHandler.WaitForEvent] before calling this method.
func (h *SummaryHandler) Error() error {
	return h.err.Get()
}

// StateSync fetches all state associated with [Summary] and applies it to disk.
// Any error is returned, and MUST be treated as fatal. After this method
// returns without error, one must call [SummaryHandler.OnFinish] to finalize
// the state sync.
func (h *SummaryHandler) StateSync(ctx context.Context, s *Summary) error {
	const (
		numBlocksToFetch   = 512 // min 256 for BLOCKHASH op, some extra for settlement...
		maxLeafRequestSize = 1024
	)

	// Recorded here, rather than in [SummaryHandler.AcceptSummary], so that a
	// sync started by a handler wrapping this one is also reported by
	// [SummaryHandler.SyncProgressOf].
	h.target.Set(s)
	h.snowCtx.Log.Info("starting state sync",
		zap.Uint64("summaryHeight", s.AcceptedHeight),
		zap.Stringer("summaryHash", s.AcceptedHash),
	)

	// Persist the snapshot-disabled marker before the sync mutates anything,
	// mirroring [evmsnapshot.Tree.Disable]: if the node dies mid-sync, the
	// restarted node's snapshot loading (saedb.NewTracker) then constructs an
	// inert tree instead of rebuilding a snapshot from the stale local state —
	// background generation that would race a resumed sync's snapshot wipe and
	// leaf writes, and discard its resume progress. A completed sync re-enables
	// the snapshot in [SummaryHandler.rawdbInvariants]. There is no generator
	// to stop here: the tracker, and with it any generation, is only
	// constructed after the sync finishes (see cchain's finishInitialize).
	disable := h.db.NewBatch()
	rawdb.WriteSnapshotDisabled(disable)
	if err := disable.Write(); err != nil {
		return fmt.Errorf("marking snapshot disabled for state sync: %w", err)
	}

	blockSyncer := syncblock.NewSyncer(
		h.snowCtx.Log,
		syncblock.NewClient(h.network.Network, h.network.PeerTracker, h.clientMetrics.blocks),
		h.db,
		h.parseBlock,
		s.AcceptedHash,
		s.AcceptedHeight,
		numBlocksToFetch,
	)
	h.snowCtx.Log.Info("syncing blocks",
		zap.Uint64("tipHeight", s.AcceptedHeight),
		zap.Int("numBlocks", numBlocksToFetch),
	)
	if err := blockSyncer.Sync(ctx); err != nil {
		return err
	}
	h.snowCtx.Log.Info("block sync finished")

	// With blocks now on disk, we can find the state root
	hdr := rawdb.ReadHeader(h.db, s.AcceptedHash, s.AcceptedHeight)
	if hdr == nil {
		return fmt.Errorf("couldn't find header %s at height %d", s.AcceptedHash, s.AcceptedHeight)
	}

	// Only wipe the snapshot if we are not resuming a sync already in
	// progress for this exact root: see wipeSnapshot's doc comment.
	shouldWipe, err := shouldWipeSnapshot(h.db, hdr.Root)
	if err != nil {
		return fmt.Errorf("checking whether to wipe snapshot: %w", err)
	}
	if shouldWipe {
		h.snowCtx.Log.Info("wiping stale snapshot before state sync",
			zap.Stringer("targetRoot", hdr.Root),
		)
		if err := h.wipeSnapshot(ctx); err != nil {
			return err
		}
	}

	codeSyncer, err := code.NewSyncer(
		h.snowCtx.Log,
		code.NewClient(h.network.Network, h.network.PeerTracker, h.clientMetrics.code),
		h.db,
	)
	if err != nil {
		return fmt.Errorf("creating code syncer: %w", err)
	}

	evmSyncer, err := evmstate.NewSyncer(
		hashdb.NewClient(
			h.snowCtx.Log,
			h.network.Network,
			p2p.EVMLeafRequestHandlerID,
			ethcommon.HashLength,
			h.network.PeerTracker,
			h.clientMetrics.stateTrieLeaves,
		),
		h.db,
		hdr.Root,
		codeSyncer,
		maxLeafRequestSize,
	)
	if err != nil {
		return fmt.Errorf("creating evm state syncer: %w", err)
	}

	h.snowCtx.Log.Info("syncing EVM state and contract code",
		zap.Stringer("stateRoot", hdr.Root),
	)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return codeSyncer.Sync(egCtx)
	})
	eg.Go(func() error {
		return evmSyncer.Sync(egCtx)
	})
	if err := eg.Wait(); err != nil {
		if finalizer, ok := evmSyncer.(synctypes.Finalizer); ok {
			err = errors.Join(err, finalizer.Finalize())
		}
		return err
	}
	h.snowCtx.Log.Info("EVM state sync finished")
	return nil
}

// shouldWipeSnapshot reports whether the snapshot must be wiped before
// syncing to targetRoot. It compares targetRoot against the sync root
// persisted by [evmstate.HashDBSyncer] (via customrawdb.WriteSyncRoot,
// read here through customrawdb.ReadSyncRoot): a differing (including
// absent) persisted root means there is nothing resumable at targetRoot, so
// the snapshot must be wiped; an equal persisted root means a prior attempt
// at this exact root is in progress, and its leaves must be kept.
//
// This mirrors the contract documented on [evmstate.NewHashDBSyncer]: "the
// caller must wipe the account and storage snapshots in db unless this run
// resumes the root already persisted there. Leaves left behind count as
// resume progress." Coreth's prepareForSync guards its own wipe the same
// way, with !isResume.
func shouldWipeSnapshot(db ethdb.KeyValueReader, targetRoot ethcommon.Hash) (bool, error) {
	persisted, err := customrawdb.ReadSyncRoot(db)
	switch {
	case errors.Is(err, database.ErrNotFound):
		persisted = ethcommon.Hash{}
	case err != nil:
		return false, err
	}
	return persisted != targetRoot, nil
}

// wipeSnapshot removes any pre-existing snapshot so post-sync snapshot reads
// cannot be served from stale layers — e.g. one partially generated by the
// pre-transition chain before an eager transition (see vms/transitionvm) —
// and resets the generator marker so the snapshot is rebuilt from the synced
// state. This mirrors coreth's prepareForSync semantics: the same four key
// kinds are wiped (the snapshot block hash, the snapshot root, every account
// snapshot entry, and every storage snapshot entry), then the generator
// marker is reset.
//
// Callers MUST first check [shouldWipeSnapshot]: wiping unconditionally would
// also discard resume progress for a sync already under way at the target
// root, contrary to the contract documented on [evmstate.NewHashDBSyncer].
//
// It deliberately does not call [evmsnapshot.WipeSnapshot]: that vendored
// helper calls log.Crit (os.Exit) on any database failure, which would crash
// the process instead of surfacing an error, violating this package's
// contract — enforced by FuzzSyncErrorSurfacedViaError — that every sync
// failure surfaces via the returned error.
func (h *SummaryHandler) wipeSnapshot(ctx context.Context) error {
	if err := customrawdb.DeleteSnapshotBlockHash(h.db); err != nil {
		return fmt.Errorf("deleting snapshot block hash: %w", err)
	}
	if err := h.db.Delete(rawdb.SnapshotRootKey); err != nil {
		return fmt.Errorf("deleting snapshot root: %w", err)
	}
	const hashLen = ethcommon.HashLength
	if err := wipeSnapshotKeyRange(ctx, h.db, rawdb.SnapshotAccountPrefix, len(rawdb.SnapshotAccountPrefix)+hashLen); err != nil {
		return fmt.Errorf("wiping account snapshot: %w", err)
	}
	if err := wipeSnapshotKeyRange(ctx, h.db, rawdb.SnapshotStoragePrefix, len(rawdb.SnapshotStoragePrefix)+2*hashLen); err != nil {
		return fmt.Errorf("wiping storage snapshot: %w", err)
	}

	w := &firstErrKeyValueWriter{KeyValueWriter: h.db}
	evmsnapshot.ResetSnapshotGeneration(w)
	return w.err
}

// wipeSnapshotKeyRange deletes every key in db with the given prefix and
// exact length keylen, skipping same-prefix keys of a different length (the
// single-byte snapshot prefixes are shared with unrelated keys, e.g. trie
// nodes). Modeled on evmsnapshot's wipeKeyRange (batched deletes, periodic
// flush and iterator recreation), but every failure is returned instead of
// triggering log.Crit/log.Error, and ctx cancellation is checked between
// flushes. Compaction and progress logging are perf-only and are skipped.
func wipeSnapshotKeyRange(ctx context.Context, db ethdb.KeyValueStore, prefix []byte, keylen int) error {
	const flushEvery = 10_000

	batch := db.NewBatch()
	it := db.NewIterator(prefix, nil)

	var items int
	for it.Next() {
		key := it.Key()
		if !bytes.HasPrefix(key, prefix) {
			break
		}
		if len(key) != keylen {
			continue
		}
		if err := batch.Delete(key); err != nil {
			it.Release()
			return err
		}
		items++

		if items%flushEvery == 0 {
			seekPos := key[len(prefix):]
			it.Release()
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()

			select {
			case <-ctx.Done():
				return context.Cause(ctx)
			default:
			}

			it = db.NewIterator(prefix, seekPos)
		}
	}
	itErr := it.Error()
	it.Release()
	if itErr != nil {
		return itErr
	}
	return batch.Write()
}

// firstErrKeyValueWriter wraps an [ethdb.KeyValueWriter], remembering only
// the first error returned by Put or Delete while always reporting success
// to the caller. It drives [evmsnapshot.ResetSnapshotGeneration], which
// otherwise calls log.Crit (os.Exit) on any write failure; the caller
// inspects err after the call instead.
type firstErrKeyValueWriter struct {
	ethdb.KeyValueWriter
	err error
}

func (w *firstErrKeyValueWriter) Put(key, value []byte) error {
	if err := w.KeyValueWriter.Put(key, value); err != nil && w.err == nil {
		w.err = err
	}
	return nil
}

func (w *firstErrKeyValueWriter) Delete(key []byte) error {
	if err := w.KeyValueWriter.Delete(key); err != nil && w.err == nil {
		w.err = err
	}
	return nil
}

func (h *SummaryHandler) OnFinish(s *Summary) error {
	lastAccepted := rawdb.ReadHeader(h.db, s.AcceptedHash, s.AcceptedHeight)
	if lastAccepted == nil {
		return errors.New("couldn't find last accepted header")
	}
	settledHeight := h.hooks.SettledBy(lastAccepted).Height
	settledHash := rawdb.ReadCanonicalHash(h.db, settledHeight)
	if settledHash == (ethcommon.Hash{}) {
		return fmt.Errorf("no canonical hash for settled block at height %d", settledHeight)
	}
	lastSettled := rawdb.ReadBlock(h.db, settledHash, settledHeight)
	if lastSettled == nil {
		return fmt.Errorf("couldn't find last settled block at height %d", settledHeight)
	}

	h.snowCtx.Log.Info("finalizing state sync",
		zap.Uint64("lastAcceptedHeight", s.AcceptedHeight),
		zap.Uint64("settledHeight", settledHeight),
	)

	if err := h.persistExecutionResults(lastSettled, lastAccepted); err != nil {
		return err
	}

	if err := h.updateBloomIndexer(lastAccepted); err != nil {
		return fmt.Errorf("updating bloom indexer: %w", err)
	}

	// MUST be called last since rawdb markers signal success.
	if err := h.rawdbInvariants(lastSettled.Header(), lastAccepted); err != nil {
		return fmt.Errorf("rawdb invariants failed: %w", err)
	}

	return nil
}

func (h *SummaryHandler) persistExecutionResults(lastSettled *types.Block, lastAccepted *types.Header) (retErr error) {
	gt, err := hook.SettledGasTime(h.hooks, lastSettled.Header(), lastAccepted)
	if err != nil {
		return fmt.Errorf("couldn't calculate settled gas time: %w", err)
	}

	xdb, err := h.hooks.ExecutionResultsDB(
		filepath.Join(h.snowCtx.ChainDataDir, sae.ExecutionResultsDir),
	)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, xdb.Close())
	}()

	block, err := blocks.New(lastSettled, nil, nil, h.hooks, h.snowCtx.Log)
	if err != nil {
		return fmt.Errorf("creating block for last settled: %w", err)
	}

	blackhole := new(atomic.Pointer[blocks.Block])
	return block.MarkExecuted(
		h.db,
		xdb,
		gt,
		time.Now(), // time only used for metrics
		nil,        // base fee is unknown
		nil,        // receipts are unknown
		lastAccepted.Root,
		blackhole, // last executed
	)
}

// Assumes that settler.Number is a multiple of [params.BloomBitsBlocks].
func (h *SummaryHandler) updateBloomIndexer(settler *types.Header) error {
	const sectionSize = params.BloomBitsBlocks
	idx := core.NewBloomIndexer(h.db, sectionSize, 0)
	section := (settler.Number.Uint64() - 1) / sectionSize
	idx.AddCheckpoint(section, settler.ParentHash)
	return idx.Close()
}

func (h *SummaryHandler) rawdbInvariants(settled, settler *types.Header) error {
	batch := h.db.NewBatch()
	rawdb.WriteHeadFastBlockHash(batch, settler.Hash())
	rawdb.WriteHeadHeaderHash(batch, settled.Hash())
	rawdb.WriteHeadBlockHash(batch, settled.Hash())
	rawdb.WriteFinalizedBlockHash(batch, settled.Hash())
	rawdb.WriteSnapshotRoot(batch, settler.Root) // post-execution settled
	// Re-enable the snapshot that [SummaryHandler.StateSync] marked disabled:
	// the synced leaves are now complete for the root written above.
	rawdb.DeleteSnapshotDisabled(batch)
	return batch.Write()
}

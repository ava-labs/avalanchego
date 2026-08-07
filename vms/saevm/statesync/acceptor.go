// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"

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
	if s.height == 0 {
		// The genesis block is already accepted, so we don't need to do anything.
		return block.StateSyncSkipped, nil
	}

	// If any blocks have been accepted, don't state sync.
	hash, err := h.lastAcceptedHash()
	if err != nil {
		return block.StateSyncSkipped, err
	}
	if height := rawdb.ReadHeaderNumber(h.db, hash); height != nil && *height > 0 {
		return block.StateSyncSkipped, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return block.StateSyncSkipped, nil
	}
	// The sync must outlive this request-scoped ctx, so it gets its own; the
	// CancelFunc is owned by the run and fired by [SummaryHandler.Shutdown].
	var syncCtx context.Context
	syncCtx, h.cancel = context.WithCancel(context.Background())
	go func() {
		defer h.cancel()
		defer close(h.done) // result barrier: h.err is now readable
		h.err = h.stateSync(syncCtx, s)
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

// Error blocks until the sync goroutine has finished, then returns the error
// that terminated it.
func (h *SummaryHandler) Error(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-h.done:
		return h.err
	}
}

func (h *SummaryHandler) stateSync(ctx context.Context, s *Summary) error {
	const numBlocksToFetch = 512 // min 256 for op code, some extra for settlement...

	blockSyncer, err := syncblock.NewSyncer(
		h.snowCtx.Log,
		syncblock.NewClient(h.network.Network, h.network.PeerTracker),
		h.db,
		s.blockHash,
		s.height,
		numBlocksToFetch,
	)
	if err != nil {
		return err
	}
	if err := blockSyncer.Sync(ctx); err != nil {
		return err
	}

	// With blocks now on disk, we can find the state root
	hdr := rawdb.ReadHeader(h.db, s.blockHash, s.height)
	if hdr == nil {
		return fmt.Errorf("couldn't find header %s at height %d", s.blockHash, s.height)
	}

	codeQueue, err := code.NewQueue(h.db)
	if err != nil {
		return fmt.Errorf("creating code queue: %w", err)
	}

	codeSyncer := code.NewSyncer(
		h.snowCtx.Log,
		code.NewClient(h.network.Network, h.network.PeerTracker),
		h.db,
		codeQueue.CodeHashes(),
	)

	evmSyncer, err := hashdb.NewEVMSyncer(h.snowCtx.Log,
		hashdb.NewClient(h.network.Network, h.network.TrieDependentTracker, p2p.EVMLeafRequestHandlerID),
		h.db,
		hdr.Root,
		codeQueue,
	)
	if err != nil {
		return fmt.Errorf("creating evm state syncer: %w", err)
	}
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return codeSyncer.Sync(egCtx)
	})
	eg.Go(func() error {
		return evmSyncer.Sync(egCtx)
	})
	if err := eg.Wait(); err != nil {
		return errors.Join(
			fmt.Errorf("syncing state: %w", err),
			evmSyncer.Finalize(),
		)
	}

	return h.afterSync(s)
}

func (h *SummaryHandler) afterSync(s *Summary) error {
	lastAccepted := rawdb.ReadHeader(h.db, s.blockHash, s.height)
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

	if err := h.persistExecutionResults(lastSettled, lastAccepted); err != nil {
		return err
	}

	if err := h.rawdbInvariants(lastSettled.Header(), lastAccepted); err != nil {
		return fmt.Errorf("rawdb invariants failed: %w", err)
	}

	return h.updateBloomIndexer(lastAccepted)
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
		if err := xdb.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("closing execution results db: %w", err))
		}
	}()

	block, err := blocks.New(lastSettled, nil, nil, h.snowCtx.Log)
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

func (h *SummaryHandler) rawdbInvariants(settled, settler *types.Header) error {
	batch := h.db.NewBatch()
	rawdb.WriteHeadFastBlockHash(batch, settler.Hash())
	rawdb.WriteHeadHeaderHash(batch, settled.Hash())
	rawdb.WriteHeadBlockHash(batch, settled.Hash())
	rawdb.WriteFinalizedBlockHash(batch, settled.Hash())
	rawdb.WriteSnapshotRoot(batch, settler.Root) // post-execution settled
	return batch.Write()
}

// Assumes that [settler.Number] is a multiple of [params.BloomBitsBlocks].
func (h *SummaryHandler) updateBloomIndexer(settler *types.Header) error {
	const sectionSize = params.BloomBitsBlocks
	idx := core.NewBloomIndexer(h.db, sectionSize, 0)
	section := (settler.Number.Uint64() - 1) / sectionSize
	idx.AddCheckpoint(section, settler.ParentHash)
	return idx.Close()
}

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

	"github.com/ava-labs/avalanchego/graft/evm/sync/client/leafproto"
	"github.com/ava-labs/avalanchego/graft/evm/sync/evmstate"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"

	syncblock "github.com/ava-labs/avalanchego/vms/evm/sync/block"
	vmsevmstate "github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
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

		if err := h.StateSync(ctx, s); err != nil {
			h.err.Set(fmt.Errorf("state sync failed: %w", err))
			return
		}
		h.err.Set(h.OnFinish(s))
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

// ShouldAcceptSummary returns true if the summary should be state synced to,
// given the current disk state.
//
// TODO(alarso16): Find a way to wire through Firewood.
func (h *SummaryHandler) ShouldAcceptSummary(s *Summary) (bool, error) {
	if h.cfg.DBConfig.Scheme == customrawdb.FirewoodScheme {
		h.snowCtx.Log.Warn("State sync is not supported with Firewood scheme")
		return false, nil
	}

	if s.AcceptedHeight == 0 {
		// The genesis block is already accepted, so we don't need to do anything.
		return false, nil
	}

	// If any blocks have been accepted, don't state sync.
	hash, err := h.lastAcceptedHash()
	if err != nil {
		return false, err
	}
	if height := rawdb.ReadHeaderNumber(h.db, hash); height != nil && *height > 0 {
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
	const numBlocksToFetch = 512 // min 256 for BLOCKHASH op, some extra for settlement...

	blockSyncer := syncblock.NewSyncer(
		h.snowCtx.Log,
		syncblock.NewClient(h.network.Network, h.network.PeerTracker),
		h.db,
		h.parseBlock,
		s.AcceptedHash,
		s.AcceptedHeight,
		numBlocksToFetch,
	)
	if err := blockSyncer.Sync(ctx); err != nil {
		return err
	}

	// With blocks now on disk, we can find the state root
	hdr := rawdb.ReadHeader(h.db, s.AcceptedHash, s.AcceptedHeight)
	if hdr == nil {
		return fmt.Errorf("couldn't find header %s at height %d", s.AcceptedHash, s.AcceptedHeight)
	}

	codeSyncer, err := code.NewSyncer(
		h.snowCtx.Log,
		code.NewClient(h.network.Network, h.network.PeerTracker),
		h.db,
	)
	if err != nil {
		return fmt.Errorf("creating code syncer: %w", err)
	}

	evmSyncer, err := evmstate.NewHashDBSyncer(
		h.snowCtx.Log,
		leafproto.NewClient(
			h.snowCtx.Log,
			vmsevmstate.NewClient(
				h.network.Network,
				p2p.EVMLeafRequestHandlerID,
				h.network.TrieDependentTracker,
			),
		),
		h.db,
		hdr.Root,
		codeSyncerAdaptor{codeSyncer},
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
	return batch.Write()
}

type codeSyncerAdaptor struct {
	cs *code.Syncer
}

func (a codeSyncerAdaptor) AddCode(ctx context.Context, hashes []ethcommon.Hash) error {
	return a.cs.AddCode(hashes)
}

func (a codeSyncerAdaptor) CloseInput() {
	a.cs.DoneAdding()
}

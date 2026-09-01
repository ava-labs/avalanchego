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

	"github.com/ava-labs/avalanchego/graft/evm/sync/evmstate"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"

	synctypes "github.com/ava-labs/avalanchego/graft/evm/sync/types"
	syncblock "github.com/ava-labs/avalanchego/vms/evm/sync/block"
	ethcommon "github.com/ava-labs/libevm/common"
)

// StateSyncEnabled checks whether the node should query for state summaries.
func (h *SummaryHandler) StateSyncEnabled(context.Context) (bool, error) {
	return h.cfg.Enabled, nil
}

// ShouldAcceptSummary reports whether the summary should be state synced to,
// given the current disk state. It is called under the chain's context lock
// inside [block.StateSummary.Accept] and MUST stay cheap: disk reads only,
// no network, no side effects.
//
// TODO(alarso16): Find a way to wire through Firewood.
func (h *SummaryHandler) ShouldAcceptSummary(_ context.Context, s *Summary) (bool, error) {
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

// Sync satisfies [adaptor.SyncableVM]; it is [SummaryHandler.SyncWith] and
// no extra work.
func (h *SummaryHandler) Sync(ctx context.Context, s *Summary) error {
	return h.SyncWith(ctx, s, nil)
}

// SyncWith fetches and finalizes all state for s, leaving a runnable VM on
// nil return. A non-nil extra runs after block, code, and EVM state sync
// have completed — it may read synced headers and blocks from the database —
// and before the finalization markers are written.
func (h *SummaryHandler) SyncWith(ctx context.Context, s *Summary, extra func(context.Context) error) error {
	if err := h.fetch(ctx, s); err != nil {
		return fmt.Errorf("state sync failed: %w", err)
	}
	if extra != nil {
		if err := extra(ctx); err != nil {
			return err
		}
	}
	return h.finish(s)
}

// fetch is the fetch phase of [SummaryHandler.SyncWith]: it fetches all state
// associated with [Summary] and applies it to disk. Any error is returned,
// and MUST be treated as fatal.
func (h *SummaryHandler) fetch(ctx context.Context, s *Summary) error {
	const (
		numBlocksToFetch   = 512 // min 256 for BLOCKHASH op, some extra for settlement...
		maxLeafRequestSize = 1024
	)

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

	evmSyncer, err := evmstate.NewSyncer(
		hashdb.NewClient(
			h.snowCtx.Log,
			h.network.Network,
			p2p.EVMLeafRequestHandlerID,
			ethcommon.HashLength,
			h.network.PeerTracker,
		),
		h.db,
		hdr.Root,
		codeSyncer,
		maxLeafRequestSize,
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
		if finalizer, ok := evmSyncer.(synctypes.Finalizer); ok {
			err = errors.Join(err, finalizer.Finalize())
		}
		return err
	}
	return nil
}

// finish MUST run last: the rawdb markers it writes signal a completed sync.
func (h *SummaryHandler) finish(s *Summary) error {
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
	return batch.Write()
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"

	codequeue "github.com/ava-labs/avalanchego/graft/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/block"
	ethcommon "github.com/ava-labs/libevm/common"
)

// StateSyncEnabled checks whether the node should query for state summaries.
func (h *SummaryHandler) StateSyncEnabled(context.Context) (bool, error) {
	enabled := h.cfg.Enabled
	if enabled == nil {
		// if any blocks have been processed, don't state sync
		return rawdb.ReadHeadHeader(h.db) == nil, nil
	}
	return *enabled, nil
}

// AcceptSummary performs the entire state sync given the provided summary. If
// state sync is not enabled, it returns StateSyncSkipped. Once the state sync
// is complete, [SummaryHandler.WaitForEvent] will return [common.StateSyncDone].
func (h *SummaryHandler) AcceptSummary(ctx context.Context, s *Summary) (smblock.StateSyncMode, error) {
	enabled, err := h.StateSyncEnabled(ctx)
	if err != nil || !enabled {
		return smblock.StateSyncSkipped, err
	}

	ctx, h.cancelSync = context.WithCancel(ctx)

	go func() {
		defer h.cancelSync()
		defer close(h.stateSyncDone)
		if err := h.stateSync(ctx, s); err != nil {
			h.syncErr = err
			return
		}

		if err := h.afterSync(ctx, s); err != nil {
			h.syncErr = err
		}
	}()

	return smblock.StateSyncStatic, nil
}

// WaitForEvent blocks until the state sync is complete, or the context is
// canceled. If the state sync completes, [common.StateSyncDone] is returned.
func (h *SummaryHandler) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-h.stateSyncDone:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

// Error returns the error that terminated state sync, if a sync was started and
// has since finished.
func (h *SummaryHandler) Error() error {
	select {
	case <-h.stateSyncDone:
		// only ever written before the channel is closed
		return h.syncErr
	default:
		return nil
	}
}

func (h *SummaryHandler) stateSync(ctx context.Context, s *Summary) error {
	const numBlocksToFetch = 512 // min 256 for op code, some extra for settlement...

	blockSyncer, err := block.NewSyncer(h.network.Network, h.network.PeerTracker, h.db, s.blockHash, s.height, block.Config{
		Log:                    h.snowCtx.Log,
		BlocksToFetch:          numBlocksToFetch,
		ExtraBlockVerification: h.cfg.ExtraBlockVerification,
	})
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

	codeQueue, err := codequeue.NewQueue(h.db)
	if err != nil {
		return fmt.Errorf("creating code queue: %w", err)
	}

	codeSyncer, err := code.NewSyncer(h.network.Network, h.network.PeerTracker, h.db, codeQueue, code.Config{
		Log: h.snowCtx.Log,
	})
	if err != nil {
		return fmt.Errorf("creating code syncer: %w", err)
	}

	evmSyncer, err := hashdb.NewEVMSyncer(h.network.Network, h.network.PeerTracker, h.db, hdr.Root, codeQueue, hashdb.Config{
		Log: h.snowCtx.Log,
	})
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
		return fmt.Errorf("syncing state: %w", err)
	}

	return nil
}

func (h *SummaryHandler) afterSync(ctx context.Context, s *Summary) error {
	lastAccepted := rawdb.ReadHeader(h.db, s.blockHash, s.height)
	if lastAccepted == nil {
		return fmt.Errorf("couldn't find last accepted header")
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
	if err := block.PersistStateSynced(h.db, xdb, lastAccepted.Root, gt); err != nil {
		return fmt.Errorf("persisting state synced: %w", err)
	}
	return nil
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

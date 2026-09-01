// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/statesync"
)

// StateSyncEnabled checks whether the node should query for state summaries.
func (h *Handler) StateSyncEnabled(context.Context) (bool, error) {
	return h.cfg.Enabled, nil
}

// WaitForEvent blocks until the entire state sync is complete.
func (h *Handler) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-h.done:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

// Error blocks until the entire state sync (the embedded handler's and the
// atomic trie's) has finished, then returns the error that terminated it.
func (h *Handler) Error() error {
	return h.err.Get()
}

// AcceptSummary delegates to the embedded handler and, once its sync
// completes, syncs the atomic trie state.
//
// AcceptSummary MUST only be called once.
func (h *Handler) AcceptSummary(ctx context.Context, summary *summary) (block.StateSyncMode, error) {
	evmSyncer := h.Handler.Syncer()
	shouldSync := evmSyncer.ShouldAcceptSummary(&summary.summary)
	if !shouldSync {
		return block.StateSyncSkipped, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return block.StateSyncSkipped, nil
	}

	// Recorded before the sync goroutine starts, so a sync is never observable
	// through its side effects without also being observable in the metrics.
	h.Handler.MarkSyncStarted(&summary.summary)

	ctx, h.cancel = context.WithCancel(context.Background())
	go func() {
		defer h.cancel()
		defer close(h.done) // result barrier: h.err is now readable

		err := h.sync(ctx, evmSyncer, summary)
		// Marked after the sync's final write and before done closes, so that
		// an observer that saw the sync finish also sees its outcome.
		h.Handler.MarkSyncFinished(err)
		h.err.Set(err)
	}()
	return block.StateSyncStatic, nil
}

// sync runs the full sync for the accepted summary: the EVM state, the atomic
// trie state, and the finalizing writes.
func (h *Handler) sync(ctx context.Context, evmSyncer *statesync.Syncer, summary *summary) error {
	if err := evmSyncer.Sync(ctx, &summary.summary); err != nil {
		return err
	}
	if err := h.syncCChainState(ctx, summary); err != nil {
		return err
	}
	return evmSyncer.WriteSynced(&summary.summary)
}

func (h *Handler) syncCChainState(ctx context.Context, s *summary) error {
	settledHeight, err := h.settledHeight(s.summary.AcceptedHash, s.summary.AcceptedHeight)
	if err != nil {
		return err
	}

	syncer := state.NewSyncer(h.network.Network, h.network.PeerTracker, h.state, s.settledRoot, settledHeight, h.atomicLeaves)
	return syncer.Sync(ctx)
}

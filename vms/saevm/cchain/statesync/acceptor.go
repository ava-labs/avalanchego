// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"go.uber.org/zap"

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

// SyncError returns any error that has occurred thusfar during state sync.
func (h *Handler) SyncError() error {
	return h.err.Get()
}

// AcceptSummary ensures the summary should be accepted. If it shouldn't, it
// returns [block.StateSyncSkipped]. Otherwise, it asynchronouosly begins the
// state sync. [Handler.WaitForEvent] will return [common.StateSyncDone] once
// the sync is complete. Any error from during the state sync can be read via
// [Handler.SyncError].
//
// AcceptSummary MUST only be called once.
func (h *Handler) AcceptSummary(ctx context.Context, s *summary) (block.StateSyncMode, error) {
	evmSyncer := h.Handler.Syncer()
	shouldSync := evmSyncer.ShouldAcceptSummary(&s.summary)
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
	h.Handler.MarkSyncStarted(&s.summary)

	// The sync runs in a goroutine that outlives this call, but callers
	// idiomatically cancel ctx on return. Drop that cancellation while
	// keeping ctx's values, so the sync stays in the caller's trace.
	ctx, h.cancel = context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		defer h.cancel()
		defer close(h.done) // result barrier: h.err is now readable

		err := h.sync(ctx, evmSyncer, s)
		// Marked after the sync's final write and before done closes, so that
		// an observer that saw the sync finish also sees its outcome.
		h.Handler.MarkSyncFinished(err)
		h.err.Set(err)
	}()
	return block.StateSyncStatic, nil
}

// sync performs the full state sync, including the EVM sync and the cross-chain
// state sync, followed by the finalizing writes.
func (h *Handler) sync(ctx context.Context, evmSyncer *statesync.Syncer, s *summary) error {
	if err := evmSyncer.Sync(ctx, &s.summary); err != nil {
		return err
	}
	if err := h.syncCChainState(ctx, s); err != nil {
		return err
	}
	return evmSyncer.WriteSynced(&s.summary)
}

// syncCChainState syncs the cross-chain state at the settled height. It MUST
// only be called after the EVM sync, as the settled height can only be
// determined from the last accepted block, which is fetched during the EVM
// sync.
func (h *Handler) syncCChainState(ctx context.Context, s *summary) error {
	settledHeight, err := h.settledHeight(s.summary.AcceptedHash, s.summary.AcceptedHeight)
	if err != nil {
		return err
	}

	h.snowCtx.Log.Info("syncing cross-chain state",
		zap.Stringer("settledCrossChainRoot", s.settledRoot),
		zap.Uint64("settledHeight", settledHeight),
		zap.Stringer("acceptedHash", s.summary.AcceptedHash),
		zap.Uint64("acceptedHeight", s.summary.AcceptedHeight),
	)
	crossChainSyncer := state.NewSyncer(h.network.Network, h.network.PeerTracker, h.state, s.settledRoot, settledHeight, h.atomicLeaves)
	if err := crossChainSyncer.Sync(ctx); err != nil {
		return err
	}
	h.snowCtx.Log.Info("finished syncing cross-chain state")
	return nil
}

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
	evmSyncer := statesync.NewSyncer(
		h.cfg,
		h.hooks,
		h.snowCtx,
		h.network,
		h.ethDB,
	)
	shouldSync := evmSyncer.ShouldAcceptSummary(&s.summary)
	if !shouldSync {
		return block.StateSyncSkipped, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return block.StateSyncSkipped, nil
	}

	// The sync runs in a goroutine that outlives this call, but callers
	// idiomatically cancel ctx on return. Drop that cancellation while
	// keeping ctx's values, so the sync stays in the caller's trace.
	ctx, h.cancel = context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		defer h.cancel()
		defer close(h.done) // result barrier: h.err is now readable

		h.err.Set(h.sync(ctx, evmSyncer, s))
	}()
	return block.StateSyncStatic, nil
}

// sync performs the full state sync, including the EVM sync and the cross-chain
// state sync.
func (h *Handler) sync(ctx context.Context, evmSyncer *statesync.Syncer, s *summary) error {
	if err := evmSyncer.Sync(ctx, &s.summary); err != nil {
		return err
	}

	// We can only determine the settled height after we have fetched the last
	// accepted block, which is fetched during the EVM sync.
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
	crossChainSyncer := state.NewSyncer(h.network.Network, h.network.PeerTracker, h.state, s.settledRoot, settledHeight)
	if err := crossChainSyncer.Sync(ctx); err != nil {
		return err
	}
	h.snowCtx.Log.Info("finished syncing cross-chain state")

	return evmSyncer.WriteSynced(&s.summary)
}

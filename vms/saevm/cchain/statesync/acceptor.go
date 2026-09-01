// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
)

// WaitForEvent blocks until the entire state sync is complete.
func (h *SummaryHandler) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-h.done:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

// Error blocks until the entire state sync (the embedded handler's and the
// atomic trie's) has finished, then returns the error that terminated it.
func (h *SummaryHandler) Error() error {
	return h.err.Get()
}

// AcceptSummary delegates to the embedded handler and, once its sync
// completes, syncs the atomic trie state.
//
// AcceptSummary MUST only be called once.
func (h *SummaryHandler) AcceptSummary(ctx context.Context, summary *summary) (block.StateSyncMode, error) {
	shouldSync, err := h.SummaryHandler.ShouldAcceptSummary(&summary.summary)
	if err != nil || !shouldSync {
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
		defer close(h.done) // result barrier: h.err is now readable

		// Failures are logged at Info: the error is surfaced to the engine
		// via [SummaryHandler.Error], which treats it as fatal.
		if err := h.SummaryHandler.StateSync(ctx, &summary.summary); err != nil {
			h.log.Info("state sync failed", zap.Error(err))
			h.err.Set(err)
			return
		}
		if err := h.syncCChainState(ctx, summary); err != nil {
			h.log.Info("atomic trie sync failed", zap.Error(err))
			h.err.Set(err)
			return
		}
		if err := h.OnFinish(&summary.summary); err != nil {
			h.log.Info("state sync finalization failed", zap.Error(err))
			h.err.Set(err)
			return
		}
		h.log.Info("state sync finished")
	}()
	return block.StateSyncStatic, nil
}

func (h *SummaryHandler) syncCChainState(ctx context.Context, s *summary) error {
	settledHeight, err := h.settledHeight(s.summary.AcceptedHash, s.summary.AcceptedHeight)
	if err != nil {
		return err
	}

	h.log.Info("syncing atomic trie",
		zap.Stringer("atomicRoot", s.settledRoot),
		zap.Uint64("settledHeight", settledHeight),
	)
	syncer := state.NewSyncer(h.network.Network, h.network.PeerTracker, h.state, s.settledRoot, settledHeight, h.atomicLeaves)
	if err := syncer.Sync(ctx); err != nil {
		return err
	}
	h.log.Info("atomic trie sync finished")
	return nil
}

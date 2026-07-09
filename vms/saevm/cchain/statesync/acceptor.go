// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
)

// WaitForEvent blocks until the entire state sync is complete.
func (h *SummaryHandler) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-h.stateSyncDone:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

// Error blocks until the entire state sync (the embedded handler's and the
// atomic trie's) has finished, then returns the error that terminated it.
func (h *SummaryHandler) Error(ctx context.Context) error {
	select {
	case <-h.stateSyncDone:
		return h.err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// AcceptSummary delegates to the embedded handler and, once its sync
// completes, syncs the atomic trie state. [SummaryHandler.WaitForEvent]
// reports completion of the whole pipeline and [SummaryHandler.Error] its
// terminal error.
//
// AcceptSummary MUST only be called once.
func (h *SummaryHandler) AcceptSummary(ctx context.Context, summary *summary) (block.StateSyncMode, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return block.StateSyncSkipped, nil
	}

	mode, err := h.SummaryHandler.AcceptSummary(ctx, &summary.summary)
	if err != nil || mode == block.StateSyncSkipped {
		return mode, err
	}

	// The sync must outlive this request-scoped ctx, so it gets its own; the
	// CancelFunc is fired by [SummaryHandler.Shutdown].
	syncCtx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() {
		defer cancel()
		defer close(h.stateSyncDone) // result barrier: h.err is now readable

		h.err = h.SummaryHandler.Error(syncCtx)
		if h.err != nil {
			if !errors.Is(h.err, context.Canceled) {
				h.log.Error("state sync failed; skipping cross-chain state sync", zap.Error(h.err))
			}
			return
		}
		h.err = h.stateSync(syncCtx, summary)
	}()
	return mode, nil
}

func (h *SummaryHandler) stateSync(ctx context.Context, summary *summary) error {
	settledHeight, err := h.settledHeight(summary.summary.BlockHash(), summary.Height())
	if err != nil {
		return err
	}

	syncer := state.NewSyncer(h.network.Network, h.network.PeerTracker, h.state, summary.settledRoot, settledHeight)
	return syncer.Sync(ctx)
}

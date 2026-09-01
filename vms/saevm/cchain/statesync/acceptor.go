// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

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

// ShouldAcceptSummary reports whether the summary should be state synced to,
// given the current disk state.
func (h *SummaryHandler) ShouldAcceptSummary(ctx context.Context, s *summary) (bool, error) {
	return h.SummaryHandler.ShouldAcceptSummary(ctx, &s.summary)
}

// Sync runs the SAE state sync with the atomic-trie sync spliced in before
// finalization. The closure runs after block sync, so it can read the
// settled header from the database.
func (h *SummaryHandler) Sync(ctx context.Context, s *summary) error {
	return h.SummaryHandler.SyncWith(ctx, &s.summary, func(ctx context.Context) error {
		settledHeight, err := h.settledHeight(s.summary.AcceptedHash, s.summary.AcceptedHeight)
		if err != nil {
			return err
		}
		return state.NewSyncer(h.network.Network, h.network.PeerTracker, h.state, s.settledRoot, settledHeight).Sync(ctx)
	})
}

// AcceptSummary delegates to [SummaryHandler.Sync] in the background.
//
// AcceptSummary MUST only be called once.
func (h *SummaryHandler) AcceptSummary(ctx context.Context, summary *summary) (block.StateSyncMode, error) {
	shouldSync, err := h.ShouldAcceptSummary(ctx, summary)
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
		h.err.Set(h.Sync(ctx, summary))
	}()
	return block.StateSyncStatic, nil
}

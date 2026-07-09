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
	evmSyncer := statesync.NewSyncer(
		h.cfg,
		h.hooks,
		h.snowCtx,
		h.network,
		h.ethDB,
	)
	shouldSync := evmSyncer.ShouldAcceptSummary(&summary.summary)
	if !shouldSync {
		return block.StateSyncSkipped, nil
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

		if err := evmSyncer.Sync(ctx, &summary.summary); err != nil {
			h.err.Set(err)
			return
		}
		if err := h.syncCChainState(ctx, summary); err != nil {
			h.err.Set(err)
			return
		}
		h.err.Set(evmSyncer.WriteSynced(&summary.summary))
	}()
	return block.StateSyncStatic, nil
}

func (h *Handler) syncCChainState(ctx context.Context, s *summary) error {
	settledHeight, err := h.settledHeight(s.summary.AcceptedHash, s.summary.AcceptedHeight)
	if err != nil {
		return err
	}

	syncer := state.NewSyncer(h.network.Network, h.network.PeerTracker, h.state, s.settledRoot, settledHeight)
	return syncer.Sync(ctx)
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
)

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

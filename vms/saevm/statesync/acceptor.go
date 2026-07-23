// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"github.com/ava-labs/libevm/core/rawdb"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// StateSyncEnabled checks whether the node should query for state summaries.
func (h *SummaryHandler) StateSyncEnabled(context.Context) (bool, error) {
	enabled := h.cfg.Enabled
	if enabled != nil {
		return *enabled, nil
	}

	// If any blocks have been accepted, don't state sync. Acceptance, not
	// execution, is the signal here: the head header only advances on
	// execution, which lags acceptance.
	hash, ok := h.lastAcceptedHash()
	if !ok {
		return true, nil
	}
	height := rawdb.ReadHeaderNumber(h.db, hash)
	return height == nil || *height == 0, nil
}

// AcceptSummary performs the entire state sync given the provided summary. If
// state sync is not enabled, it returns StateSyncSkipped. Once the state sync
// is complete, [SummaryHandler.WaitForEvent] will return [common.StateSyncDone].
func (h *SummaryHandler) AcceptSummary(ctx context.Context, _ *Summary) (block.StateSyncMode, error) {
	enabled, err := h.StateSyncEnabled(ctx)
	if err != nil || !enabled {
		return block.StateSyncSkipped, err
	}

	go func() {
		// TODO(alarso16): implement state sync
		close(h.stateSyncDone)
	}()

	return block.StateSyncStatic, nil
}

// WaitForEvent blocks until the state sync is complete, or the context is
// canceled. Once the state sync is done, [common.StateSyncDone] is returned.
func (h *SummaryHandler) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-h.stateSyncDone:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

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
	if enabled == nil {
		// if any blocks have been processed, don't state sync
		return rawdb.ReadHeadHeader(h.db) == nil, nil
	}
	return *enabled, nil
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
// canceled. If the state sync completes, [common.StateSyncDone] is returned.
func (h *SummaryHandler) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-h.stateSyncDone:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

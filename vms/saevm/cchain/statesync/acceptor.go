// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
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

// AcceptSummary delegates to the embedded handler, which owns the state-sync
// completion signal observed by the promoted WaitForEvent.
func (h *SummaryHandler) AcceptSummary(ctx context.Context, summary *summary) (block.StateSyncMode, error) {
	mode, err := h.SummaryHandler.AcceptSummary(ctx, &summary.summary)
	if err != nil || mode == block.StateSyncSkipped {
		return mode, err
	}

	go func() {
		defer close(h.stateSyncDone)
		// TODO(alarso16): implement state sync with a synchronous statesync.SummaryHandler API.
	}()
	return mode, nil
}

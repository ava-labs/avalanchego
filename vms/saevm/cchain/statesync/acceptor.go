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
		// must block until initial state sync done
		// TODO(alarso16): if there's an error in the first state sync, it should bubble up here
		_, err := h.SummaryHandler.WaitForEvent(ctx)
		if err != nil {
			return
		}
		// TODO(alarso16): implement state sync
	}()
	return mode, nil
}

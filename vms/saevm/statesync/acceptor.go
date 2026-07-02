// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"github.com/ava-labs/libevm/core/rawdb"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// StateSyncEnabled implements [adaptor.StateSyncable].
func (h *SummaryHandler) StateSyncEnabled(context.Context) (bool, error) {
	return h.cfg.Enabled == nil || *h.cfg.Enabled, nil
}

// AcceptSummary implements [adaptor.StateSyncable].
func (h *SummaryHandler) AcceptSummary(ctx context.Context, _ *Summary) (block.StateSyncMode, error) {
	switch enabled := h.cfg.Enabled; {
	case enabled == nil:
		// if any blocks have been processed, don't state sync
		if rawdb.ReadHeadHeader(h.db) != nil {
			return block.StateSyncSkipped, nil
		}
	case !*enabled:
		return block.StateSyncSkipped, nil
	}

	go func() {
		// TODO(alarso16): implement state sync
		close(h.stateSyncDone)
	}()

	return block.StateSyncStatic, nil
}

func (h *SummaryHandler) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-h.stateSyncDone:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

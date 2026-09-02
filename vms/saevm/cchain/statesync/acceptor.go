// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// StateSyncEnabled checks whether the node should query for state summaries.
func (h *SummaryHandler) StateSyncEnabled(context.Context) (bool, error) {
	return h.cfg.Enabled, nil
}

// AcceptSummary is not yet implemented. It always returns
// [block.StateSyncSkipped] and no error.
//
// TODO(alarso16): Implement full state sync.
func (*SummaryHandler) AcceptSummary(context.Context, *summary) (block.StateSyncMode, error) {
	return block.StateSyncSkipped, nil
}

// WaitForEvent is not yet implemented. It always blocks until the context is
// canceled and returns its error.
//
// TODO(alarso16): Implement full state sync.
func (*SummaryHandler) WaitForEvent(ctx context.Context) (common.Message, error) {
	<-ctx.Done()
	return 0, context.Cause(ctx)
}

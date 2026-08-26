// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// AcceptSummary is not yet implemented. It always returns
// [block.StateSyncSkipped] and no error.
//
// TODO(alarso16): Implement full state sync.
func (*SummaryHandler) AcceptSummary(context.Context, *summary) (block.StateSyncMode, error) {
	return block.StateSyncSkipped, nil
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (*SummaryHandler) AcceptSummary(context.Context, *Summary) (block.StateSyncMode, error) {
	panic("unimplemented")
}

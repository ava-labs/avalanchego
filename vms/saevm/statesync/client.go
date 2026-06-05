// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// GetOngoingSyncStateSummary implements [adaptor.StateSync].
func (*VM[T]) GetOngoingSyncStateSummary(context.Context) (*summary, error) {
	panic("unimplemented")
}

// ParseStateSummary implements [adaptor.StateSync].
func (*VM[T]) ParseStateSummary(context.Context, []byte) (*summary, error) {
	panic("unimplemented")
}

// AcceptSummary implements [adaptor.StateSync].
func (*VM[T]) AcceptSummary(context.Context, *summary) (block.StateSyncMode, error) {
	panic("unimplemented")
}

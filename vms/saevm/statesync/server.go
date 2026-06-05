// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import "context"

// GetLastStateSummary implements [adaptor.StateSync].
func (*VM[T]) GetLastStateSummary(context.Context) (*summary, error) {
	panic("unimplemented")
}

// GetStateSummary implements [adaptor.StateSync].
func (*VM[T]) GetStateSummary(context.Context, uint64) (*summary, error) {
	panic("unimplemented")
}

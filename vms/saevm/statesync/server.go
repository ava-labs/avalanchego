// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import "context"

// GetLastStateSummary implements [adaptor.SyncableVM].
func (*VM[T]) GetLastStateSummary(context.Context) (*summary, error) {
	panic("unimplemented")
}

// GetOngoingSyncStateSummary implements [adaptor.SyncableVM].
func (*VM[T]) GetOngoingSyncStateSummary(context.Context) (*summary, error) {
	panic("unimplemented")
}

// GetStateSummary implements [adaptor.SyncableVM].
func (*VM[T]) GetStateSummary(context.Context, uint64) (*summary, error) {
	panic("unimplemented")
}

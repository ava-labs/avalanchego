// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import "context"

type SummaryHandler struct{}

func (*SummaryHandler) StateSyncEnabled(context.Context) (bool, error) {
	panic("unimplemented")
}

func (*SummaryHandler) ParseStateSummary(context.Context, []byte) (*Summary, error) {
	panic("unimplemented")
}

func (*SummaryHandler) GetLastStateSummary(context.Context) (*Summary, error) {
	panic("unimplemented")
}

func (*SummaryHandler) GetOngoingSyncStateSummary(context.Context) (*Summary, error) {
	panic("unimplemented")
}

func (*SummaryHandler) GetStateSummary(context.Context, uint64) (*Summary, error) {
	panic("unimplemented")
}

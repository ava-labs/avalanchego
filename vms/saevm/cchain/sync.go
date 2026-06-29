// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// TODO(#5513): Implement C-Chain state sync.
type syncer struct{}

func (*syncer) StateSyncEnabled(context.Context) (bool, error) {
	return false, nil
}

func (*syncer) GetOngoingSyncStateSummary(context.Context) (stateSummary, error) {
	return stateSummary{}, database.ErrNotFound
}

func (*syncer) GetLastStateSummary(context.Context) (stateSummary, error) {
	return stateSummary{}, database.ErrNotFound
}

func (*syncer) GetStateSummary(context.Context, uint64) (stateSummary, error) {
	return stateSummary{}, database.ErrNotFound
}

func (*syncer) ParseStateSummary(context.Context, []byte) (stateSummary, error) {
	return stateSummary{}, block.ErrStateSyncableVMNotImplemented
}

func (*syncer) AcceptSummary(context.Context, stateSummary) (block.StateSyncMode, error) {
	return block.StateSyncSkipped, block.ErrStateSyncableVMNotImplemented
}

type stateSummary struct{}

func (stateSummary) ID() ids.ID {
	panic("unimplemented")
}

func (stateSummary) Bytes() []byte {
	panic("unimplemented")
}

func (stateSummary) Height() uint64 {
	panic("unimplemented")
}

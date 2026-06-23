// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package cchain implements the C-Chain VM atop [sae.VM]. It composes the
// C-Chain block-building hooks, the cross-chain transaction pool, and the avax
// JSON-RPC service that ingests Export and Import transactions.
package cchain

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (*VM) StateSyncEnabled(context.Context) (bool, error) {
	return false, nil
}

type StateSummary struct{}

func (s StateSummary) ID() ids.ID {
	panic("unimplemented")
}

func (s StateSummary) Bytes() []byte {
	panic("unimplemented")
}

func (s StateSummary) Height() uint64 {
	panic("unimplemented")
}

func (*VM) GetLastStateSummary(context.Context) (StateSummary, error) {
	return StateSummary{}, block.ErrStateSyncableVMNotImplemented
}

func (*VM) GetOngoingSyncStateSummary(context.Context) (StateSummary, error) {
	return StateSummary{}, block.ErrStateSyncableVMNotImplemented
}

func (*VM) GetStateSummary(context.Context, uint64) (StateSummary, error) {
	return StateSummary{}, block.ErrStateSyncableVMNotImplemented
}

func (*VM) ParseStateSummary(context.Context, []byte) (StateSummary, error) {
	return StateSummary{}, block.ErrStateSyncableVMNotImplemented
}

func (*VM) AcceptSummary(context.Context, StateSummary) (block.StateSyncMode, error) {
	return block.StateSyncSkipped, block.ErrStateSyncableVMNotImplemented
}

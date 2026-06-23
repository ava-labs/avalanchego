// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ block.StateSyncableVM = (*VM)(nil)

func (v *VM) StateSyncEnabled(ctx context.Context) (bool, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.StateSyncEnabled(ctx)
}

func (v *VM) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.GetOngoingSyncStateSummary(ctx)
}

func (v *VM) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.GetLastStateSummary(ctx)
}

func (v *VM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.ParseStateSummary(ctx, summaryBytes)
}

func (v *VM) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	return v.current.chain.GetStateSummary(ctx, summaryHeight)
}

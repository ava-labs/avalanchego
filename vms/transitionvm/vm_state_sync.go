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

	ssVM, ok := v.current.chain.(block.StateSyncableVM)
	if !ok {
		return false, nil
	}
	return ssVM.StateSyncEnabled(ctx)
}

func (v *VM) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	ssVM, ok := v.current.chain.(block.StateSyncableVM)
	if !ok {
		return nil, block.ErrStateSyncableVMNotImplemented
	}
	return ssVM.GetOngoingSyncStateSummary(ctx)
}

func (v *VM) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	ssVM, ok := v.current.chain.(block.StateSyncableVM)
	if !ok {
		return nil, block.ErrStateSyncableVMNotImplemented
	}
	return ssVM.GetLastStateSummary(ctx)
}

func (v *VM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	ssVM, ok := v.current.chain.(block.StateSyncableVM)
	if !ok {
		return nil, block.ErrStateSyncableVMNotImplemented
	}
	return ssVM.ParseStateSummary(ctx, summaryBytes)
}

func (v *VM) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	ssVM, ok := v.current.chain.(block.StateSyncableVM)
	if !ok {
		return nil, block.ErrStateSyncableVMNotImplemented
	}
	return ssVM.GetStateSummary(ctx, summaryHeight)
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *VM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.AppGossip(ctx, nodeID, msg)
}

func (vm *VM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.AppRequest(ctx, nodeID, requestID, deadline, request)
}

func (vm *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.HealthCheck(ctx)
}

func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.LastAccepted(ctx)
}

func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.GetBlockIDAtHeight(ctx, height)
}

func (vm *VM) SetPreference(ctx context.Context, blkID ids.ID) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.SetPreference(ctx, blkID)
}

func (vm *VM) SetPreferenceWithContext(ctx context.Context, blkID ids.ID, blockCtx *block.Context) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.SetPreferenceWithContext(ctx, blkID, blockCtx)
}

func (vm *VM) Version(ctx context.Context) (string, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.Version(ctx)
}

func (vm *VM) Shutdown(ctx context.Context) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.Shutdown(ctx)
}

var _ block.StateSyncableVM = (*VM)(nil)

func (vm *VM) StateSyncEnabled(ctx context.Context) (bool, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.StateSyncEnabled(ctx)
}

func (vm *VM) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.GetOngoingSyncStateSummary(ctx)
}

func (vm *VM) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.GetLastStateSummary(ctx)
}

func (vm *VM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.ParseStateSummary(ctx, summaryBytes)
}

func (vm *VM) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.GetStateSummary(ctx, summaryHeight)
}

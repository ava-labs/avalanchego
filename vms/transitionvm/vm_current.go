// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// All these functions just route through to the current chain.

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
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.current.chain.HealthCheck(ctx)
}

func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.current.chain.LastAccepted(ctx)
}

func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.current.chain.GetBlockIDAtHeight(ctx, height)
}

func (vm *VM) Version(ctx context.Context) (string, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.current.chain.Version(ctx)
}

func (vm *VM) Shutdown(ctx context.Context) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.current.chain.Shutdown(ctx)
}

func (vm *VM) StateSyncEnabled(ctx context.Context) (bool, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.current.chain.StateSyncEnabled(ctx)
}

func (vm *VM) GetOngoingSyncStateSummary(ctx context.Context) (smblock.StateSummary, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.current.chain.GetOngoingSyncStateSummary(ctx)
}

func (vm *VM) GetLastStateSummary(ctx context.Context) (smblock.StateSummary, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.current.chain.GetLastStateSummary(ctx)
}

func (vm *VM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (smblock.StateSummary, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.current.chain.ParseStateSummary(ctx, summaryBytes)
}

func (vm *VM) GetStateSummary(ctx context.Context, summaryHeight uint64) (smblock.StateSummary, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.current.chain.GetStateSummary(ctx, summaryHeight)
}

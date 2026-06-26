// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	vm.consensusState = state
	return vm.current.chain.SetState(ctx, state)
}

func (vm *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Stop waiting if the VM transitions.
	removeCancel := context.AfterFunc(vm.current.ctx, cancel)

	// Unregister the hook on return so it doesn't leak.
	defer removeCancel()

	return vm.current.chain.WaitForEvent(ctx)
}

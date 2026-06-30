// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	vm.consensusState.Set(state)
	return vm.current.chain.SetState(ctx, state)
}

func (vm *VM) SetPreference(ctx context.Context, blkID ids.ID) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	vm.setPreference.Set(true)
	return vm.current.chain.SetPreference(ctx, blkID)
}

func (vm *VM) SetPreferenceWithContext(ctx context.Context, blkID ids.ID, blockCtx *smblock.Context) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	vm.setPreference.Set(true)
	return vm.current.chain.SetPreferenceWithContext(ctx, blkID, blockCtx)
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

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
	return vm.withLocks(func() error {
		vm.consensusState.Set(state)
		return vm.current.chain.SetState(ctx, state)
	})
}

func (vm *VM) SetPreference(ctx context.Context, blkID ids.ID) error {
	return vm.withLocks(func() error {
		vm.preferenceSet.Set(true)
		return vm.current.chain.SetPreference(ctx, blkID)
	})
}

func (vm *VM) SetPreferenceWithContext(ctx context.Context, blkID ids.ID, blockCtx *smblock.Context) error {
	return vm.withLocks(func() error {
		vm.preferenceSet.Set(true)
		return vm.current.chain.SetPreferenceWithContext(ctx, blkID, blockCtx)
	})
}

func (vm *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	done := make(chan struct{})
	defer close(done)

	// don't access [VM.current] in the goroutine, as we no longer hold the
	// transition lock there.
	transitioning := vm.current.transitioning

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-done:
		case <-transitioning:
		}
		cancel()
	}()

	return vm.current.chain.WaitForEvent(ctx)
}

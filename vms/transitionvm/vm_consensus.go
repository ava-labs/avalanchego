// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

func (v *VM) SetState(ctx context.Context, state snow.State) error {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	v.current.consensusState = state
	return v.current.chain.SetState(ctx, state)
}

func (v *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// If the VM is being transitioned, we want to stop waiting on an event
	// notification.
	removeCancel := context.AfterFunc(v.current.ctx, cancel)

	// If the VM isn't transitioned, we must remove the cancellation of the
	// context to avoid a memory leak.
	defer removeCancel()

	return v.current.chain.WaitForEvent(ctx)
}

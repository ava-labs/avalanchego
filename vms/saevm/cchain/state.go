// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/statesync"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	if state >= snow.Bootstrapping {
		var err error
		vm.onBootstrappingOnce.Do(func() {
			err = vm.onBootstrapping(ctx)
		})
		if err != nil {
			return err
		}

		if err := vm.VM.SetState(ctx, state); err != nil {
			return err
		}
	}

	vm.mode.Set(state)
	return nil
}

var (
	_ StateDependent = (*sae.VM)(nil)
	_ StateDependent = (*statesync.SummaryHandler)(nil)
)

type StateDependent interface {
	GetBlock(context.Context, ids.ID) (*blocks.Block, error)
	GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error)
	LastAccepted(context.Context) (ids.ID, error)
}

func (vm *VM) activeHandler() StateDependent {
	if vm.mode.Get() >= snow.Bootstrapping {
		return vm.VM
	}
	return vm.SummaryHandler
}

func (vm *VM) GetBlock(ctx context.Context, id ids.ID) (*blocks.Block, error) {
	return vm.activeHandler().GetBlock(ctx, id)
}

func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	return vm.activeHandler().GetBlockIDAtHeight(ctx, height)
}

func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	return vm.activeHandler().LastAccepted(ctx)
}

func (vm *VM) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
	if vm.mode.Get() == snow.StateSyncing {
		return vm.SummaryHandler.WaitForEvent(ctx)
	}

	// TODO(StephenButtolph): Do not busy loop with [snowcommon.PendingTxs]. The
	// txpools are cleared after block execution, so we may still have
	// transactions in the txpool while blocks containing those transactions are
	// processing.

	// TODO(StephenButtolph): Wait until the minimum block delay has passed.

	ctx, cancel := context.WithCancel(ctx)
	type result struct {
		msg snowcommon.Message
		err error
	}
	results := make(chan result, 2)
	go func() {
		defer cancel()
		msg, err := vm.VM.WaitForEvent(ctx)
		results <- result{msg, err}
	}()
	go func() {
		defer cancel()
		err := vm.txpool.AwaitTxs(ctx)
		results <- result{snowcommon.PendingTxs, err}
	}()

	r := <-results
	return r.msg, r.err
}

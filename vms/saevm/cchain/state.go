// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/statesync"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// SetState sets the state of the VM. If the state is transitioning to
// [snow.Bootstrapping], the full VM will be initialized. Any error returned
// is fatal.
func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	if state >= snow.Bootstrapping {
		var err error
		vm.onBootstrappingOnce.Do(func() {
			err = vm.onBootstrapping(ctx)
		})
		if err != nil {
			return err
		}
	}

	if err := vm.VM.SetState(ctx, state); err != nil {
		return fmt.Errorf("setting sae.VM state: %w", err)
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

// WaitForEvent waits until the ACP-226 minimum block delay since the preferred
// block has elapsed, then waits for a transaction to be in the txpool or for
// the SAE VM to produce an event.
func (vm *VM) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
	switch vm.mode.Get() {
	case snow.Initializing:
		// no event can occur while the VM is initializing.
		<-ctx.Done()
		return 0, ctx.Err()
	case snow.StateSyncing:
		return vm.SummaryHandler.WaitForEvent(ctx)
	}

	// Throttle to avoid busy looping: the txpools only clear after block
	// execution, so pending txs can re-signal while their block is processing.
	//
	// TODO(JonathanOppenheimer): The txpool should track preference / reorgs so
	// we don't need this throttle.
	throttleUntil := vm.lastWaitForEvent.Get().Add(minWaitForEventDelay)

	// Pace block building on the ACP-226 minimum block delay so that the event
	// sources are consulted when we are actually willing to build.
	buildTime := earliestBuildTime(vm.VM.GetPreference())

	until := throttleUntil
	if buildTime.After(until) {
		until = buildTime
	}
	if err := vm.waitUntil(ctx, until); err != nil {
		return 0, err
	}

	// Race the SAE event source against the cross-chain txpool. The winner's
	// deferred cancel unblocks the loser, whose pending call returns and delivers
	// its discarded result to the buffered channel, so neither goroutine leaks.
	raceCtx, cancel := context.WithCancel(ctx)
	type result struct {
		msg snowcommon.Message
		err error
	}
	results := make(chan result, 2)
	go func() {
		defer cancel()
		msg, err := vm.VM.WaitForEvent(raceCtx)
		results <- result{msg, err}
	}()
	go func() {
		defer cancel()
		err := vm.txpool.AwaitTxs(raceCtx)
		results <- result{snowcommon.PendingTxs, err}
	}()

	r := <-results
	if r.err == nil {
		vm.lastWaitForEvent.Set(vm.now())
	}
	return r.msg, r.err
}

// minWaitForEventDelay is the minimum spacing between consecutive
// [VM.WaitForEvent] returns. 100ms isn't special here, it was selected as a
// reasonable frequency for the engine to poll on whether to build a block or
// not.
const minWaitForEventDelay = 100 * time.Millisecond

// earliestBuildTime returns the earliest wall-clock time at which a child of b
// may be built.
func earliestBuildTime(b *blocks.Block) time.Time {
	h := b.Header()
	return blockTime(h).Add(delayExponent(h).DelayDuration())
}

// waitUntil blocks until [VM.now] reaches t, returning early with the
// cancellation cause if ctx is canceled first.
func (vm *VM) waitUntil(ctx context.Context, t time.Time) error {
	timeToWait := t.Sub(vm.now())
	if timeToWait <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-time.After(timeToWait):
		return nil
	}
}

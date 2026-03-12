// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package engine

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/evm/message"
)

var _ Executor = (*dynamicExecutor)(nil)

// dynamicExecutor runs syncers concurrently with block queueing.
// It wraps [Coordinator] to manage the sync lifecycle.
type dynamicExecutor struct {
	coordinator *Coordinator
}

func newDynamicExecutor(registry *SyncerRegistry, acceptor Acceptor, pivotInterval uint64) *dynamicExecutor {
	coordinator := NewCoordinator(
		registry,
		Callbacks{
			FinalizeVM: acceptor.AcceptSync,
			OnDone:     nil, // Set in Execute to capture completion.
		},
		WithPivotInterval(pivotInterval),
	)
	return &dynamicExecutor{coordinator: coordinator}
}

// Execute launches the coordinator and blocks until sync completes or fails.
func (d *dynamicExecutor) Execute(ctx context.Context, summary message.Syncable) error {
	done := make(chan error, 1)

	// Wire up OnDone to signal completion.
	d.coordinator.callbacks.OnDone = func(err error) {
		if err != nil {
			log.Error("dynamic state sync completed with error", "err", err)
		} else {
			log.Info("dynamic state sync completed successfully")
		}
		done <- err
	}

	d.coordinator.Start(ctx, summary)
	return <-done
}

// OnBlockAccepted enqueues the block for deferred processing and updates the sync target.
func (d *dynamicExecutor) OnBlockAccepted(b EthBlockWrapper) (bool, error) {
	if d.coordinator.CurrentState() == StateExecutingBatch {
		// During batch replay the block is already being executed directly
		// by executeBlockOperations. Re-enqueueing here would cause an
		// infinite loop (Accept -> OnEngineAccept -> enqueue -> dequeue -> Accept ...).
		return false, nil
	}

	if !d.enqueue(b, OpAccept) {
		return false, nil
	}

	if b == nil || b.GetEthBlock() == nil {
		return true, nil
	}

	ethBlock := b.GetEthBlock()
	target := newSyncTarget(ethBlock.Hash(), ethBlock.Root(), ethBlock.NumberU64())
	if err := d.coordinator.UpdateSyncTarget(target); err != nil {
		// Block is enqueued but target update failed.
		return true, fmt.Errorf("block enqueued but sync target update failed: %w", err)
	}
	return true, nil
}

// OnBlockRejected enqueues the block for deferred rejection.
func (d *dynamicExecutor) OnBlockRejected(b EthBlockWrapper) (bool, error) {
	if d.coordinator.CurrentState() == StateExecutingBatch {
		return false, nil
	}
	return d.enqueue(b, OpReject), nil
}

// OnBlockVerified enqueues the block for deferred verification.
func (d *dynamicExecutor) OnBlockVerified(b EthBlockWrapper) (bool, error) {
	if d.coordinator.CurrentState() == StateExecutingBatch {
		return false, nil
	}
	return d.enqueue(b, OpVerify), nil
}

// enqueue adds a block operation to the coordinator's queue.
func (d *dynamicExecutor) enqueue(b EthBlockWrapper, op BlockOperationType) bool {
	ok := d.coordinator.AddBlockOperation(b, op)
	if !ok {
		if b != nil && b.GetEthBlock() != nil {
			ethBlock := b.GetEthBlock()
			log.Warn("could not enqueue block operation",
				"hash", ethBlock.Hash(),
				"height", ethBlock.NumberU64(),
				"op", op.String(),
			)
		}
	}
	return ok
}

// CurrentState returns the coordinator's current state.
func (d *dynamicExecutor) CurrentState() State {
	return d.coordinator.CurrentState()
}

// UpdateSyncTarget updates the coordinator's sync target.
func (d *dynamicExecutor) UpdateSyncTarget(target message.Syncable) error {
	return d.coordinator.UpdateSyncTarget(target)
}

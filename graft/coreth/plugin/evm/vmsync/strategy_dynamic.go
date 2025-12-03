// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
)

var _ SyncStrategy = (*dynamicStrategy)(nil)

// dynamicStrategy runs syncers concurrently with block queueing.
// It wraps [Coordinator] to manage the sync lifecycle.
type dynamicStrategy struct {
	coordinator *Coordinator
}

func newDynamicStrategy(registry *SyncerRegistry, finalizer *finalizer, pivotInterval uint64) *dynamicStrategy {
	coordinator := NewCoordinator(
		registry,
		Callbacks{
			FinalizeVM: finalizer.finalize,
			OnDone:     nil, // Set in Start to capture completion.
		},
		WithPivotInterval(pivotInterval),
	)
	return &dynamicStrategy{coordinator: coordinator}
}

// Start launches the coordinator and blocks until sync completes or fails.
func (d *dynamicStrategy) Start(ctx context.Context, summary message.Syncable) error {
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
func (d *dynamicStrategy) OnBlockAccepted(b EthBlockWrapper) (bool, error) {
	if d.coordinator.CurrentState() == StateExecutingBatch {
		// Still enqueue for the next batch, but don't update target.
		return d.enqueue(b, OpAccept), nil
	}

	if !d.enqueue(b, OpAccept) {
		return false, nil
	}

	ethb := b.GetEthBlock()
	target := newSyncTarget(ethb.Hash(), ethb.Root(), ethb.NumberU64())
	if err := d.coordinator.UpdateSyncTarget(target); err != nil {
		// Block is enqueued but target update failed.
		return true, fmt.Errorf("block enqueued but sync target update failed: %w", err)
	}
	return true, nil
}

// OnBlockRejected enqueues the block for deferred rejection.
func (d *dynamicStrategy) OnBlockRejected(b EthBlockWrapper) (bool, error) {
	return d.enqueue(b, OpReject), nil
}

// OnBlockVerified enqueues the block for deferred verification.
func (d *dynamicStrategy) OnBlockVerified(b EthBlockWrapper) (bool, error) {
	return d.enqueue(b, OpVerify), nil
}

// enqueue adds a block operation to the coordinator's queue.
func (d *dynamicStrategy) enqueue(b EthBlockWrapper, op BlockOperationType) bool {
	ok := d.coordinator.AddBlockOperation(b, op)
	if !ok {
		if ethb := b.GetEthBlock(); ethb != nil {
			log.Warn("could not enqueue block operation",
				"hash", ethb.Hash(),
				"height", ethb.NumberU64(),
				"op", op.String(),
			)
		}
	}
	return ok
}

// CurrentState returns the coordinator's current state.
func (d *dynamicStrategy) CurrentState() State {
	return d.coordinator.CurrentState()
}

// UpdateSyncTarget updates the coordinator's sync target.
func (d *dynamicStrategy) UpdateSyncTarget(target message.Syncable) error {
	return d.coordinator.UpdateSyncTarget(target)
}

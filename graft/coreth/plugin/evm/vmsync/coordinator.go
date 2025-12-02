// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/libevm/libevm/options"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
)

// State represents the lifecycle phases of dynamic state sync orchestration.
type State int

const (
	StateIdle State = iota
	StateInitializing
	StateRunning
	StateFinalizing
	StateExecutingBatch
	StateCompleted
	StateAborted
)

var (
	errInvalidTargetType    = errors.New("invalid target type")
	errInvalidState         = errors.New("invalid coordinator state")
	errBatchCancelled       = errors.New("batch execution cancelled")
	errBatchOperationFailed = errors.New("batch operation failed")
)

// Callbacks allows the coordinator to delegate VM-specific work back to the client.
type Callbacks struct {
	// FinalizeVM performs the same actions as finishSync/commitVMMarkers in the client.
	// The context is used for cancellation checks during finalization.
	FinalizeVM func(ctx context.Context, target message.Syncable) error
	// OnDone is called when the coordinator finishes (successfully or with error).
	OnDone func(err error)
}

// Coordinator orchestrates dynamic state sync across multiple syncers.
type Coordinator struct {
	// state is managed atomically to allow cheap concurrent checks/updates.
	state atomic.Int32
	// target stores the current target [message.Syncable] when [Coordinator.UpdateSyncTarget] is called.
	target         atomic.Value
	queue          *blockQueue
	syncerRegistry *SyncerRegistry
	callbacks      Callbacks
	// doneOnce ensures [Callbacks.OnDone] is invoked at most once.
	doneOnce sync.Once

	// pivotInterval configures the pivot policy throttling. 0 disables throttling.
	pivotInterval uint64
	pivot         *pivotPolicy
}

// CoordinatorOption follows the functional options pattern for Coordinator.
type CoordinatorOption = options.Option[Coordinator]

// WithPivotInterval configures the interval-based pivot policy. 0 disables it.
func WithPivotInterval(interval uint64) CoordinatorOption {
	return options.Func[Coordinator](func(co *Coordinator) {
		co.pivotInterval = interval
	})
}

// NewCoordinator constructs a coordinator to orchestrate dynamic state sync across multiple syncers.
func NewCoordinator(syncerRegistry *SyncerRegistry, cbs Callbacks, opts ...CoordinatorOption) *Coordinator {
	co := &Coordinator{
		queue:          newBlockQueue(),
		syncerRegistry: syncerRegistry,
		callbacks:      cbs,
	}
	options.ApplyTo(co, opts...)
	co.state.Store(int32(StateIdle))

	return co
}

// Start launches all syncers and returns immediately. Failures are monitored
// in the background and will transition to [StateAborted].
func (co *Coordinator) Start(ctx context.Context, initial message.Syncable) {
	co.state.Store(int32(StateInitializing))
	co.target.Store(initial)
	co.pivot = newPivotPolicy(co.pivotInterval)

	cctx, cancel := context.WithCancelCause(ctx)
	g := co.syncerRegistry.StartAsync(cctx, initial)

	co.state.Store(int32(StateRunning))

	go func() {
		if err := g.Wait(); err != nil {
			co.finish(cancel, err)
			return
		}
		// All syncers finished successfully: finalize syncers, then finalize VM and execute the queued batch.
		if err := co.syncerRegistry.FinalizeAll(cctx); err != nil {
			co.finish(cancel, err)
			return
		}
		if err := co.ProcessQueuedBlockOperations(cctx); err != nil {
			co.finish(cancel, err)
			return
		}
		co.finish(cancel, nil)
	}()
}

// ProcessQueuedBlockOperations finalizes the VM and processes queued block operations
// in FIFO order. Called after syncers complete to finalize state and execute deferred operations.
func (co *Coordinator) ProcessQueuedBlockOperations(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	co.state.Store(int32(StateFinalizing))

	if co.callbacks.FinalizeVM != nil {
		if err := ctx.Err(); err != nil {
			co.state.Store(int32(StateAborted))
			return err
		}

		loaded := co.target.Load()
		current, ok := loaded.(message.Syncable)
		if !ok {
			co.state.Store(int32(StateAborted))
			return errInvalidTargetType
		}
		if err := co.callbacks.FinalizeVM(ctx, current); err != nil {
			co.state.Store(int32(StateAborted))
			return err
		}
	}

	if err := ctx.Err(); err != nil {
		co.state.Store(int32(StateAborted))
		return err
	}

	co.state.Store(int32(StateExecutingBatch))

	if err := co.executeBlockOperationBatch(ctx); err != nil {
		return err
	}

	return nil
}

// UpdateSyncTarget broadcasts a new target to all syncers and removes stale blocks from queue.
// Only valid in [StateRunning] state. Syncers manage cancellation themselves.
func (co *Coordinator) UpdateSyncTarget(newTarget message.Syncable) error {
	if co.CurrentState() != StateRunning {
		return errInvalidState
	}
	if !co.pivot.shouldForward(newTarget.Height()) {
		return nil
	}

	// Re-check state before modifying queue to handle concurrent transitions.
	if co.CurrentState() != StateRunning {
		return errInvalidState
	}

	// Remove blocks from queue that will never be executed (behind the new target).
	co.queue.removeBelowHeight(newTarget.Height())

	co.target.Store(newTarget)

	if err := co.syncerRegistry.UpdateSyncTarget(newTarget); err != nil {
		return err
	}
	co.pivot.advance()
	return nil
}

// AddBlockOperation appends the block to the queue while in the Running or
// StateExecutingBatch state. Blocks enqueued during batch execution will be
// processed in the next batch. Returns true if the block was queued, false
// if the queue was already sealed or the block is nil.
func (co *Coordinator) AddBlockOperation(b EthBlockWrapper, op BlockOperationType) bool {
	if b == nil {
		return false
	}
	state := co.CurrentState()
	if state != StateRunning && state != StateExecutingBatch {
		return false
	}
	return co.queue.enqueue(b, op)
}

func (co *Coordinator) CurrentState() State {
	return State(co.state.Load())
}

// executeBlockOperationBatch executes queued block operations in FIFO order.
// Partial completion is acceptable as operations are idempotent.
func (co *Coordinator) executeBlockOperationBatch(ctx context.Context) error {
	operations := co.queue.dequeueBatch()
	for i, op := range operations {
		select {
		case <-ctx.Done():
			return fmt.Errorf("operation %d/%d: %w", i+1, len(operations), errors.Join(errBatchCancelled, ctx.Err()))
		default:
		}

		var err error
		switch op.operation {
		case OpAccept:
			err = op.block.Accept(ctx)
		case OpReject:
			err = op.block.Reject(ctx)
		case OpVerify:
			err = op.block.Verify(ctx)
		}
		if err != nil {
			return fmt.Errorf("operation %d/%d (%v): %w", i+1, len(operations), op.operation, errors.Join(errBatchOperationFailed, err))
		}
	}
	return nil
}

func (co *Coordinator) finish(cancel context.CancelCauseFunc, err error) {
	if err != nil {
		co.state.Store(int32(StateAborted))
	} else {
		co.state.Store(int32(StateCompleted))
	}
	if cancel != nil {
		cancel(err)
	}
	if co.callbacks.OnDone != nil {
		co.doneOnce.Do(func() { co.callbacks.OnDone(err) })
	}
}

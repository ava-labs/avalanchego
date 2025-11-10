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

	// pivot policy to throttle [Coordinator.UpdateSyncTarget] calls.
	pivot *pivotPolicy

	pivotInterval uint64
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

	co.pivot = newPivotPolicy(co.pivotInterval)
	co.state.Store(int32(StateIdle))

	return co
}

// Start launches all syncers and returns immediately. Failures are monitored
// in the background and will transition to [StateAborted].
func (co *Coordinator) Start(ctx context.Context, initial message.Syncable) {
	co.state.Store(int32(StateInitializing))
	co.target.Store(initial)

	cctx, cancel := context.WithCancelCause(ctx)
	g := co.syncerRegistry.StartAsync(cctx, initial)

	co.state.Store(int32(StateRunning))

	go func() {
		if err := g.Wait(); err != nil {
			co.finish(cancel, err)
			return
		}
		// All syncers finished successfully: finalize VM and execute the queued batch.
		if err := co.ProcessQueuedBlockOperations(cctx); err != nil {
			co.finish(cancel, err)
			return
		}
		co.finish(cancel, nil)
	}()
}

// ProcessQueuedBlockOperations finalizes the VM at the current target and processes the
// queued operations in FIFO order. Intended to be called after a target update
// cycle when it's time to process the queued operations.
func (co *Coordinator) ProcessQueuedBlockOperations(ctx context.Context) error {
	// Check for cancellation before starting finalization phase.
	if err := ctx.Err(); err != nil {
		return err
	}

	co.state.Store(int32(StateFinalizing))

	if co.callbacks.FinalizeVM != nil {
		loaded := co.target.Load()
		current, ok := loaded.(message.Syncable)
		if !ok {
			return errInvalidTargetType
		}
		// FinalizeVM should complete atomically. The context is passed for internal
		// cancellation checks, but the coordinator expects completion or an error.
		if err := co.callbacks.FinalizeVM(ctx, current); err != nil {
			return err
		}
	}

	co.state.Store(int32(StateExecutingBatch))

	// Execute queued block operations sequentially. Each operation can be
	// cancelled individually, but the batch execution itself is not atomic - partial completion
	// is acceptable as operations are idempotent.
	if err := co.executeBlockOperationBatch(ctx); err != nil {
		return err
	}

	return nil
}

// UpdateSyncTarget broadcasts a new target to all updatable syncers.
// It is only valid in the [StateRunning] state.
// Note: no batch execution occurs here. Batches are only executed after
// finalization.
// Note: Syncers manage cancellation themselves through their Sync() contexts.
func (co *Coordinator) UpdateSyncTarget(newTarget message.Syncable) error {
	if co.CurrentState() != StateRunning {
		return errInvalidState
	}
	// Respect pivot policy if configured.
	if !co.pivot.shouldForward(newTarget.Height()) {
		return nil
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

// executeBlockOperationBatch executes all queued block operations in FIFO order.
// Each operation can be cancelled individually, but the batch execution itself
// is not atomic - partial completion is acceptable as operations are idempotent.
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

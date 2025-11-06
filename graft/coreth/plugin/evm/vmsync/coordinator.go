// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
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
	errInvalidTargetType = errors.New("invalid target type")
	errInvalidState      = errors.New("invalid coordinator state")
)

// Callbacks allows the coordinator to delegate VM-specific work back to the client.
type Callbacks struct {
	// FinalizeVM performs the same actions as finishSync/commitVMMarkers in the client.
	FinalizeVM func(target message.Syncable) error
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
	// cancel cancels the syncers' context (with cause) when aborting or finishing.
	cancel context.CancelCauseFunc

	// pivot policy to throttle [Coordinator.UpdateSyncTarget] calls.
	pivot *pivot
}

// CoordinatorOption follows the functional options pattern for Coordinator.
type CoordinatorOption = options.Option[Coordinator]

// WithPivotInterval configures the interval-based pivot policy. 0 disables it.
func WithPivotInterval(interval uint64) CoordinatorOption {
	return options.Func[Coordinator](func(co *Coordinator) {
		if interval == 0 {
			co.pivot = nil
			return
		}
		co.pivot = newPivotPolicy(interval)
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

	cctx, cancel := context.WithCancelCause(ctx)
	co.cancel = cancel
	g := co.syncerRegistry.StartAsync(cctx, initial)

	co.state.Store(int32(StateRunning))

	go func() {
		if err := g.Wait(); err != nil {
			co.finish(err)
			return
		}
		// All syncers finished successfully: finalize VM and execute the queued batch.
		if err := co.ApplyQueuedBatch(cctx); err != nil {
			co.finish(err)
			return
		}
		co.finish(nil)
	}()
}

// ApplyQueuedBatch finalizes the VM at the current target and processes the
// queued operations in FIFO order. Intended to be called after a target update
// cycle when it's time to process the queued operations.
func (co *Coordinator) ApplyQueuedBatch(ctx context.Context) error {
	co.state.Store(int32(StateFinalizing))
	if co.callbacks.FinalizeVM != nil {
		loaded := co.target.Load()
		current, ok := loaded.(message.Syncable)
		if !ok {
			return errInvalidTargetType
		}
		if err := co.callbacks.FinalizeVM(current); err != nil {
			return err
		}
	}
	co.state.Store(int32(StateExecutingBatch))
	if co.queue != nil {
		if err := co.queue.ProcessQueue(ctx); err != nil {
			return err
		}
	}
	return nil
}

// UpdateSyncTarget broadcasts a new target to all updatable syncers.
// It is only valid in the [StateRunning] state.
// Note: no batch execution occurs here. Batches are only executed after
// finalization.
func (co *Coordinator) UpdateSyncTarget(newTarget message.Syncable) error {
	if co.CurrentState() != StateRunning {
		return errInvalidState
	}
	// Respect pivot policy if configured.
	if co.pivot != nil && !co.pivot.shouldForward(newTarget.Height()) {
		return nil
	}

	// Remove blocks from queue that will never be executed (behind the new target).
	if co.queue != nil {
		co.queue.RemoveBlocksBelowHeight(newTarget.Height())
	}

	co.target.Store(newTarget)

	if err := co.syncerRegistry.UpdateSyncTarget(newTarget); err != nil {
		return err
	}
	if co.pivot != nil {
		co.pivot.advance()
	}
	return nil
}

// AddBlock appends the block to the queue only while in the Running state.
// Returns true if the block was queued, false if the queue was already sealed
// or the block is nil.
func (co *Coordinator) AddBlockOperation(b EthBlockWrapper, op BlockOperation) bool {
	if b == nil || co.CurrentState() != StateRunning {
		return false
	}
	return co.queue.Enqueue(b, op)
}

func (co *Coordinator) CurrentState() State {
	return State(co.state.Load())
}

func (co *Coordinator) finish(err error) {
	if err != nil {
		co.state.Store(int32(StateAborted))
	} else {
		co.state.Store(int32(StateCompleted))
	}
	if co.cancel != nil {
		co.cancel(err)
	}
	if co.callbacks.OnDone != nil {
		co.doneOnce.Do(func() { co.callbacks.OnDone(err) })
	}
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/evm/message"
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
	errInvalidState         = errors.New("invalid coordinator state")
	errBatchCancelled       = errors.New("batch execution cancelled")
	errBatchOperationFailed = errors.New("batch operation failed")
	errCommitTargetRequired = errors.New("commit target not set")
)

// Callbacks delegates VM-specific work back to the client.
type Callbacks struct {
	// FinalizeVM applies the sync result to the blockchain (same as AcceptSync).
	FinalizeVM func(ctx context.Context, target message.Syncable) error
	// DrainAcceptorQueue waits for all batch-replayed blocks to be fully processed.
	DrainAcceptorQueue func()
	// OnDone signals sync completion (success or failure).
	OnDone func(err error)
}

// Coordinator orchestrates dynamic state sync across multiple syncers.
type Coordinator struct {
	// state is managed atomically to allow cheap concurrent checks/updates.
	state atomic.Int32
	// updateMu serializes [UpdateSyncTarget] calls.
	updateMu sync.Mutex
	// targetMu protects commitTarget reads/writes.
	targetMu sync.RWMutex
	// commitTarget is the latest fully accepted fanout target.
	commitTarget message.Syncable
	// targetEpoch increments after each successful commitTarget update.
	targetEpoch atomic.Uint64

	queue          *blockQueue
	syncerRegistry *SyncerRegistry
	callbacks      Callbacks

	// doneOnce ensures [Callbacks.OnDone] is invoked at most once.
	doneOnce sync.Once

	pivotInterval uint64
	pivot         *pivotPolicy

	// initial is the first sync target, used as fallback if commitTarget is nil.
	initial message.Syncable
	cancel  context.CancelCauseFunc
}

// CoordinatorOption follows the functional options pattern for Coordinator.
type CoordinatorOption = options.Option[Coordinator]

// WithPivotInterval configures the interval-based pivot policy. 0 disables custom
// interval and uses default policy behavior.
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
	co.initial = initial
	co.setCommitTarget(initial)
	co.pivot = newPivotPolicy(co.pivotInterval)

	cctx, cancel := context.WithCancelCause(ctx)
	co.cancel = cancel
	g := co.syncerRegistry.StartAsync(cctx, initial)

	co.state.Store(int32(StateRunning))

	go func() {
		err := g.Wait()
		log.Info("all syncer goroutines exited", "err", err)
		if errors.Is(err, context.Canceled) {
			err = contextCause(cctx, err)
		}
		if err == nil {
			err = co.freezeCommitTarget(cctx)
		}

		finalizeTarget := co.getCommitTarget()
		if finalizeTarget == nil {
			finalizeTarget = co.initial
		}
		co.syncerRegistry.FinalizeAll(finalizeTarget)

		if err == nil {
			err = co.ProcessQueuedBlockOperations(cctx)
		}
		co.finish(cancel, err)
	}()
}

// ProcessQueuedBlockOperations finalizes the VM at the commit target and
// replays deferred block operations in FIFO order.
func (co *Coordinator) ProcessQueuedBlockOperations(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// The caller may have already transitioned to StateFinalizing.
	if co.CurrentState() != StateFinalizing {
		if err := co.beginFinalizing(); err != nil {
			return errInvalidState
		}
	}

	target := co.getCommitTarget()
	if target == nil {
		co.markAborted()
		return errCommitTargetRequired
	}

	if co.callbacks.FinalizeVM != nil {
		if err := co.callbacks.FinalizeVM(ctx, target); err != nil {
			co.markAborted()
			return err
		}
	}

	if err := ctx.Err(); err != nil {
		co.markAborted()
		return err
	}

	if !co.state.CompareAndSwap(int32(StateFinalizing), int32(StateExecutingBatch)) {
		return errInvalidState
	}

	// Drop blocks <= commit target (already applied by FinalizeVM).
	co.queue.removeThroughHeight(target.Height())

	// Drain the queue. New enqueues during execution are picked up in subsequent iterations.
	for {
		operations := co.queue.dequeueBatch()
		if len(operations) == 0 {
			break
		}
		err := executeBlockOperations(ctx, operations)
		// Drop dedupe markers regardless of outcome.
		co.queue.forget(operations)
		if err != nil {
			co.markAborted()
			return err
		}
	}

	if co.callbacks.DrainAcceptorQueue != nil {
		co.callbacks.DrainAcceptorQueue()
	}

	return nil
}

// UpdateSyncTarget broadcasts a new target to all syncers and removes stale blocks from queue.
// Only valid in [StateRunning] state.
func (co *Coordinator) UpdateSyncTarget(newTarget message.Syncable) error {
	co.updateMu.Lock()
	defer co.updateMu.Unlock()

	if co.CurrentState() != StateRunning {
		return errInvalidState
	}
	if !co.pivot.shouldForward(newTarget.Height()) {
		return nil
	}

	if err := co.syncerRegistry.UpdateSyncTarget(newTarget); err != nil {
		co.abort(err)
		return err
	}

	co.setCommitTarget(newTarget)
	co.targetEpoch.Add(1)
	co.queue.removeBelowHeight(newTarget.Height())
	co.pivot.advance()
	return nil
}

// AddBlockOperation appends a block to the queue while in Running or ExecutingBatch.
// Returns true if queued.
func (co *Coordinator) AddBlockOperation(b EthBlockWrapper, op BlockOperationType) bool {
	if b == nil {
		return false
	}
	state := co.CurrentState()
	if state != StateRunning && state != StateExecutingBatch {
		log.Warn("AddBlockOperation rejected: coordinator not running",
			"state", int32(state), "op", op.String())
		return false
	}
	return co.queue.enqueue(b, op)
}

// CurrentState returns the current lifecycle state of the coordinator.
func (co *Coordinator) CurrentState() State {
	return State(co.state.Load())
}

func (co *Coordinator) setCommitTarget(target message.Syncable) {
	co.targetMu.Lock()
	defer co.targetMu.Unlock()
	co.commitTarget = target
}

func (co *Coordinator) getCommitTarget() message.Syncable {
	co.targetMu.RLock()
	defer co.targetMu.RUnlock()
	return co.commitTarget
}

func (co *Coordinator) markAborted() {
	for {
		state := co.CurrentState()
		if state == StateAborted || state == StateCompleted {
			return
		}
		if co.state.CompareAndSwap(int32(state), int32(StateAborted)) {
			log.Warn("coordinator aborted", "fromState", int32(state))
			return
		}
	}
}

func (co *Coordinator) beginFinalizing() error {
	for {
		switch co.CurrentState() {
		case StateRunning:
			if co.state.CompareAndSwap(int32(StateRunning), int32(StateFinalizing)) {
				return nil
			}
		case StateFinalizing:
			return nil
		default:
			return errInvalidState
		}
	}
}

// freezeCommitTarget prevents further sync target updates and ensures the
// commitTarget reflects only roots the syncers actually synced to.
func (co *Coordinator) freezeCommitTarget(cctx context.Context) error {
	co.updateMu.Lock()
	defer co.updateMu.Unlock()

	if err := co.beginFinalizing(); err != nil {
		return contextCause(cctx, err)
	}
	return nil
}

func (co *Coordinator) abort(err error) {
	co.markAborted()
	if co.cancel != nil {
		co.cancel(err)
	}
}

func (co *Coordinator) finish(cancel context.CancelCauseFunc, err error) {
	if err != nil {
		log.Error("coordinator finishing with error", "err", err)
		co.markAborted()
	} else {
		for {
			state := co.CurrentState()
			if state == StateCompleted || state == StateAborted {
				break
			}
			if co.state.CompareAndSwap(int32(state), int32(StateCompleted)) {
				log.Info("coordinator completed", "fromState", int32(state))
				break
			}
		}
	}
	if cancel != nil {
		cancel(err)
	}
	if co.callbacks.OnDone != nil {
		co.doneOnce.Do(func() { co.callbacks.OnDone(err) })
	}
}

// contextCause extracts the cancel cause, falling back to fallback if the
// cause is nil or plain context.Canceled.
func contextCause(ctx context.Context, fallback error) error {
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return fallback
}

// executeBlockOperations executes a batch of queued block operations in FIFO order.
// Partial completion is acceptable as operations are idempotent.
func executeBlockOperations(ctx context.Context, operations []blockOperation) error {
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

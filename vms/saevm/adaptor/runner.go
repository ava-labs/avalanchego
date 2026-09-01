// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package adaptor

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
)

// Runner owns the lifecycle of a node's single state-sync run. It is the one
// asynchronous boundary between the engine's [block.StateSummary.Accept] and
// the VM's synchronous [SyncableVM.Sync]: the VM constructs a Runner, passes
// it to [ConvertStateSync], and reads completion and errors from the same
// instance.
type Runner struct {
	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc

	done chan struct{}
	// err MUST only be written before done is closed and only read after.
	err utils.Atomic[error]
}

// NewRunner constructs a [Runner] with no sync running.
func NewRunner() *Runner {
	return &Runner{done: make(chan struct{})}
}

// Start runs fn in a goroutine unless the Runner was shut down or a sync is
// already running; it reports whether the sync started.
//
// The goroutine's context is detached from any caller's: the sync outlives
// the engine call that starts it and is only canceled by [Runner.Shutdown].
func (r *Runner) Start(fn func(context.Context) error) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.stopped {
		return false
	}
	r.started = true

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() {
		defer cancel()
		defer close(r.done) // result barrier: r.err is now readable
		r.err.Set(fn(ctx))
	}()
	return true
}

// WaitForEvent blocks until the sync goroutine finishes or ctx is canceled.
// It returns [common.StateSyncDone] regardless of the sync's success: the
// engine has no failure message, so the error is surfaced by [Runner.Err]
// after the engine transitions state.
func (r *Runner) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-r.done:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

// Err returns the sync result. It MUST only be called after
// [Runner.WaitForEvent] returns [common.StateSyncDone]. It is nil if no sync
// was ever started.
func (r *Runner) Err() error {
	return r.err.Get()
}

// Shutdown prevents future Starts, cancels any running sync, and waits for
// its goroutine to exit, returning early with ctx's error if ctx expires
// first.
func (r *Runner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	r.stopped = true
	cancel := r.cancel
	r.mu.Unlock()

	if cancel == nil {
		// no sync was ever started
		return nil
	}
	cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

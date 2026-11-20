// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package acceptor provides the lifecycle management of a state sync, agnostic
// to how summaries are served or synced to.
package acceptor

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/adaptor"
)

// A Syncer can fully state-sync to a summary. Each method MUST only be called
// once, in the following order:
//   - [Syncer.ShouldAcceptSummary]
//   - [Syncer.Sync]
//   - [Syncer.WriteSynced]
type Syncer[SP adaptor.SummaryProperties] interface {
	// ShouldAcceptSummary returns true if the summary should be state synced
	// to, given the current disk state.
	ShouldAcceptSummary(SP) bool
	// Sync fetches all state required by the summary. It MUST return promptly
	// once the context is canceled. Any error returned is treated as fatal.
	Sync(context.Context, SP) error
	// WriteSynced finalizes the state sync, such that the VM can start from
	// the summary. It is only called after a successful Sync.
	WriteSynced(SP) error
}

// A SyncerConstructor returns the [Syncer] used for a single call to
// [AsyncAcceptor.AcceptSummary].
type SyncerConstructor[SP adaptor.SummaryProperties] func() (Syncer[SP], error)

// AsyncAcceptor syncs to accepted summaries in the background with a [Syncer],
// whose lifecycle it manages. It provides every method of
// [adaptor.SyncableVM] other than those serving and parsing summaries.
//
// It MUST be constructed with [New].
type AsyncAcceptor[SP adaptor.SummaryProperties] struct {
	enabled   bool
	newSyncer SyncerConstructor[SP]

	// Lifecycle management
	mu      sync.Mutex
	stopped bool
	cancel  context.CancelFunc
	err     utils.Atomic[error]
	done    chan struct{}
}

// New returns an [AsyncAcceptor] that syncs with the [Syncer] returned by
// `newSyncer`. If `enabled` is false, the engine is told not to query for
// state summaries.
func New[SP adaptor.SummaryProperties](enabled bool, newSyncer SyncerConstructor[SP]) *AsyncAcceptor[SP] {
	return &AsyncAcceptor[SP]{
		enabled:   enabled,
		newSyncer: newSyncer,
		done:      make(chan struct{}),
	}
}

// StateSyncEnabled checks whether the node should query for state summaries.
func (a *AsyncAcceptor[SP]) StateSyncEnabled(context.Context) (bool, error) {
	return a.enabled, nil
}

// GetOngoingSyncStateSummary is not implemented. It always returns
// [database.ErrNotFound].
//
// TODO(alarso16): Allow resuming state sync.
func (*AsyncAcceptor[SP]) GetOngoingSyncStateSummary(context.Context) (SP, error) {
	var zero SP
	return zero, database.ErrNotFound
}

// HealthCheck returns the current health status of state syncing.
func (*AsyncAcceptor[SP]) HealthCheck(context.Context) (any, error) {
	return nil, nil
}

// WaitForEvent blocks until the entire state sync is complete.
func (a *AsyncAcceptor[SP]) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-a.done:
		return common.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

// SyncError returns any error that has occurred thusfar during state sync.
func (a *AsyncAcceptor[SP]) SyncError() error {
	return a.err.Get()
}

// AcceptSummary ensures the summary should be accepted. If it shouldn't, it
// returns [block.StateSyncSkipped]. Otherwise, it asynchronously begins the
// state sync, calling [Syncer.Sync] followed by [Syncer.WriteSynced].
// [AsyncAcceptor.WaitForEvent] will return [common.StateSyncDone] once the sync
// is complete. Any error from during the state sync can be read via
// [AsyncAcceptor.SyncError].
//
// AcceptSummary MUST only be called once.
func (a *AsyncAcceptor[SP]) AcceptSummary(ctx context.Context, summary SP) (block.StateSyncMode, error) {
	s, err := a.newSyncer()
	if err != nil {
		return 0, fmt.Errorf("creating syncer: %w", err)
	}
	if !s.ShouldAcceptSummary(summary) {
		return block.StateSyncSkipped, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return block.StateSyncSkipped, nil
	}

	// The sync runs in a goroutine that outlives this call, but callers
	// idiomatically cancel ctx on return. Drop that cancellation while
	// keeping ctx's values, so the sync stays in the caller's trace.
	ctx, a.cancel = context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		defer a.cancel()
		defer close(a.done) // result barrier: a.err is now readable

		if err := s.Sync(ctx, summary); err != nil {
			a.err.Set(err)
			return
		}
		a.err.Set(s.WriteSynced(summary))
	}()
	return block.StateSyncStatic, nil
}

// Shutdown cancels any ongoing state sync and waits for the sync goroutine to
// exit, returning early with the context's error if ctx expires first. After
// Shutdown, no new sync can be started.
func (a *AsyncAcceptor[SP]) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	a.stopped = true
	cancel := a.cancel
	a.mu.Unlock()

	if cancel == nil {
		// no sync was ever started
		return nil
	}
	cancel()
	select {
	case <-a.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

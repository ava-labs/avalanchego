// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"context"
	"errors"
	"sync"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/evm/message"
)

var (
	_ Syncer         = (*DynamicSyncer)(nil)
	_ Finalizer      = (*DynamicSyncer)(nil)
	_ TargetReporter = (*DynamicSyncer)(nil)

	errPivotRequested = errors.New("pivot requested")
)

// DynamicSyncer implements the session-restart pattern shared by all dynamic
// syncers. It owns target tracking, session cancellation, and the pivot loop,
// delegating session-specific behavior to the PivotSession.
type DynamicSyncer struct {
	session PivotSession
	name    string
	id      string

	mu            sync.Mutex
	desiredRoot   common.Hash
	desiredHeight uint64
	sessionCancel context.CancelCauseFunc
}

func NewDynamicSyncer(
	name, id string,
	session PivotSession,
	initialRoot common.Hash,
	initialHeight uint64,
) *DynamicSyncer {
	return &DynamicSyncer{
		session:       session,
		name:          name,
		id:            id,
		desiredRoot:   initialRoot,
		desiredHeight: initialHeight,
	}
}

func (d *DynamicSyncer) Name() string { return d.name }
func (d *DynamicSyncer) ID() string   { return d.id }

// Sync runs the session-restart loop. Each iteration delegates to the
// PivotSession. When UpdateTarget triggers a pivot, the current session is
// cancelled and rebuilt for the new target.
func (d *DynamicSyncer) Sync(ctx context.Context) error {
	for {
		sessionCtx, sessionCancel := context.WithCancelCause(ctx)
		d.setSessionCancel(sessionCancel)

		err := d.session.Run(sessionCtx)

		d.setSessionCancel(nil)
		sessionCancel(nil)

		if err == nil {
			return d.session.OnSessionComplete()
		}
		if !errors.Is(context.Cause(sessionCtx), errPivotRequested) {
			return err
		}

		newRoot, newHeight := d.getDesiredTarget()
		newSession, err := d.session.Rebuild(newRoot, newHeight)
		if err != nil {
			return err
		}
		d.session = newSession
	}
}

// UpdateTarget records a newer sync target. If the session's ShouldPivot
// returns true for the new root, the active session is cancelled so the
// loop restarts with a fresh session. Thread-safe and non-blocking.
func (d *DynamicSyncer) UpdateTarget(newTarget message.Syncable) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	newHeight := newTarget.Height()
	if newHeight <= d.desiredHeight {
		return nil
	}

	newRoot := newTarget.GetBlockRoot()
	if !d.session.ShouldPivot(newRoot) {
		d.desiredHeight = newHeight
		return nil
	}

	d.desiredRoot = newRoot
	d.desiredHeight = newHeight

	if d.sessionCancel != nil {
		d.sessionCancel(errPivotRequested)
	}
	return nil
}

// TargetHeight returns the latest desired height.
func (d *DynamicSyncer) TargetHeight() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.desiredHeight
}

// Finalize delegates to the current session if it implements Finalizer.
func (d *DynamicSyncer) Finalize() error {
	if f, ok := d.session.(Finalizer); ok {
		return f.Finalize()
	}
	return nil
}

// DesiredRoot returns the current desired root (exposed for testing).
func (d *DynamicSyncer) DesiredRoot() common.Hash {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.desiredRoot
}

func (d *DynamicSyncer) setSessionCancel(cancel context.CancelCauseFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sessionCancel = cancel
}

func (d *DynamicSyncer) getDesiredTarget() (common.Hash, uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.desiredRoot, d.desiredHeight
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/libevm/libevm/options"
)

// ErrPivotRequested is used as a cancellation cause when a pivot is requested.
// Session runners may treat it as a signal to stop the current session and restart.
var ErrPivotRequested = errors.New("pivot requested")

// ID is a monotonically increasing session identifier.
type ID uint64

// Tagged associates a value with a session ID (useful for session-safe channels).
type Tagged[T any] struct {
	SessionID ID
	Value     T
}

// EqualFunc reports whether two targets are equal. When provided, it is used to treat
// redundant pivots (same target) as no-ops.
type EqualFunc[T any] func(a, b T) bool

type currentSession[T any] struct {
	id     ID
	target T
	cancel context.CancelCauseFunc
}

type managerConfig[T any] struct {
	equal EqualFunc[T]
}

// Option configures a Manager.
type Option[T any] = options.Option[managerConfig[T]]

// WithEqual sets the equality function used to identify redundant pivots.
func WithEqual[T any](eq EqualFunc[T]) Option[T] {
	return options.Func[managerConfig[T]](func(c *managerConfig[T]) {
		c.equal = eq
	})
}

// WithComparableEqual is a convenience helper for comparable targets (uses ==).
func WithComparableEqual[T comparable]() Option[T] {
	return WithEqual(func(a, b T) bool { return a == b })
}

// Session is an immutable snapshot of a running sync session.
type Session[T any] struct {
	ID     ID
	Target T
}

// Manager coordinates pivot requests across session-based workflows.
//
// It does not run work itself. A session runner typically:
// - calls Start(...) to get a session snapshot + context
// - runs work under that context
// - after work stops (typically via ctx cancellation), calls RestartIfPending(...)
//
// The manager is intended as a wrapper around existing components to implement
// pivot-anytime semantics with a clear "stop -> barrier -> restart" boundary.
type Manager[T any] struct {
	cfg managerConfig[T]

	// desired holds the latest desired target for the next session.
	desired atomic.Value // T

	// cur holds the current session snapshot (zero value before Start).
	// We treat id==0 as "no session started yet".
	cur atomic.Value // currentSession[T]

	// nextID is the next session ID to allocate.
	nextID atomic.Uint64

	// pivotPending indicates there is a pending pivot request that should trigger a restart.
	pivotPending atomic.Bool

	// transitionMu serializes session creation so Start/RestartIfPending cannot race.
	transitionMu sync.Mutex
}

// NewManager constructs a Manager with an initial desired target. The first call to Start()
// uses this target.
func NewManager[T any](initialTarget T, opts ...Option[T]) *Manager[T] {
	m := &Manager[T]{}
	options.ApplyTo(&m.cfg, opts...)
	m.desired.Store(initialTarget)
	m.cur.Store(currentSession[T]{})
	return m
}

// Current returns the current session snapshot if a session has been started.
func (m *Manager[T]) Current() (Session[T], bool) {
	cs := m.cur.Load().(currentSession[T])
	if cs.id == 0 {
		return Session[T]{}, false
	}
	return Session[T]{ID: cs.id, Target: cs.target}, true
}

// DesiredTarget returns the latest desired target.
func (m *Manager[T]) DesiredTarget() T {
	return m.desired.Load().(T)
}

// Start begins a new session using the latest desired target. The returned context is
// canceled with ErrPivotRequested when RequestPivot is called.
//
// Note: we intentionally do not store context.Context inside exported structs.
func (m *Manager[T]) Start(parent context.Context) (Session[T], context.Context) {
	m.transitionMu.Lock()
	defer m.transitionMu.Unlock()

	// Clear any pending pivot before starting a new session. This avoids a race
	// where RequestPivot sets pivotPending during Start(...) and we later clear it.
	// A pivot requested after this point will set pivotPending=true and cancel the
	// returned context as expected.
	m.pivotPending.Store(false)

	target := m.desired.Load().(T)
	ctx, cancel := context.WithCancelCause(parent)

	id := ID(m.nextID.Add(1))
	cs := currentSession[T]{
		id:     id,
		target: target,
		cancel: cancel,
	}
	m.cur.Store(cs)

	return Session[T]{ID: id, Target: target}, ctx
}

// RequestPivot updates the desired target and cancels the current session (if any) with
// ErrPivotRequested. It returns true if this call requested a pivot (i.e., it was not a
// redundant pivot to the current/desired target).
func (m *Manager[T]) RequestPivot(newTarget T) bool {
	eq := m.cfg.equal
	if eq != nil {
		// Treat redundant pivots (same target) as no-ops.
		if eq(m.desired.Load().(T), newTarget) {
			return false
		}
		if s, ok := m.Current(); ok && eq(s.Target, newTarget) {
			// Pivoting back to the current target is a true no-op: keep the session running,
			// clear any pending restart, and align desired with the current target.
			m.desired.Store(newTarget)
			m.pivotPending.Store(false)
			return false
		}
	}

	m.desired.Store(newTarget)
	m.pivotPending.Store(true)

	cs := m.cur.Load().(currentSession[T])
	if cs.cancel != nil {
		cs.cancel(ErrPivotRequested)
	}
	return true
}

// RestartIfPending transitions to a new session if a pivot is pending. It must be called
// after the caller has observed the current session end (typically due to ctx cancellation).
func (m *Manager[T]) RestartIfPending(parent context.Context) (Session[T], context.Context, bool) {
	if !m.pivotPending.CompareAndSwap(true, false) {
		return Session[T]{}, nil, false
	}
	s, ctx := m.Start(parent)
	return s, ctx, true
}

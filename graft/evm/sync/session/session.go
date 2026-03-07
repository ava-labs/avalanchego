// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package session

import (
	"context"
	"errors"
	"sync"

	"github.com/ava-labs/libevm/libevm/options"
)

// ErrPivotRequested is used as a cancellation cause when a pivot is requested.
// Session runners may treat it as a signal to stop the current session and restart.
var ErrPivotRequested = errors.New("pivot requested")

// ID is a monotonically increasing session identifier.
type ID uint64

// EqualFunc reports whether two targets are equal.
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
// It does not run work itself. A typical runner:
// - calls Start(...) to get a session snapshot + context
// - runs work under that context
// - after work stops (usually via ctx cancellation), calls RestartIfPending(...)
//
// Pivot/Start/Restart transitions are serialized by [mu] to avoid missed-cancel
// windows between Start and RequestPivot.
type Manager[T any] struct {
	cfg managerConfig[T]

	mu sync.Mutex

	desired      T
	cur          currentSession[T]
	nextID       ID
	pivotPending bool
}

// NewManager constructs a Manager with an initial desired target.
func NewManager[T any](initialTarget T, opts ...Option[T]) *Manager[T] {
	m := &Manager[T]{
		desired: initialTarget,
	}
	options.ApplyTo(&m.cfg, opts...)
	return m
}

// Current returns the current session snapshot if a session has been started.
func (m *Manager[T]) Current() (Session[T], bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cur.id == 0 {
		return Session[T]{}, false
	}
	return Session[T]{ID: m.cur.id, Target: m.cur.target}, true
}

// DesiredTarget returns the latest desired target.
func (m *Manager[T]) DesiredTarget() T {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.desired
}

// Start begins a new session using the latest desired target.
func (m *Manager[T]) Start(parent context.Context) (Session[T], context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startLocked(parent)
}

func (m *Manager[T]) startLocked(parent context.Context) (Session[T], context.Context) {
	// Cancel any prior session context to avoid leaking it when the caller
	// transitions directly via Start without an intervening RequestPivot.
	// Idempotent if RequestPivot already cancelled it.
	if m.cur.cancel != nil {
		m.cur.cancel(ErrPivotRequested)
	}

	target := m.desired
	ctx, cancel := context.WithCancelCause(parent)

	m.nextID++
	m.cur = currentSession[T]{
		id:     m.nextID,
		target: target,
		cancel: cancel,
	}
	// Starting a session to [desired] satisfies any previously pending pivot.
	m.pivotPending = false

	return Session[T]{ID: m.cur.id, Target: m.cur.target}, ctx
}

// RequestPivot updates the desired target and cancels the current session (if any)
// with [ErrPivotRequested]. Returns true if this call introduced a new pivot target.
func (m *Manager[T]) RequestPivot(newTarget T) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	eq := m.cfg.equal
	if eq != nil {
		// If we are already on this target, there is no pivot to apply. Also clear any
		// stale pending restart and realign desired.
		if m.cur.id != 0 && eq(m.cur.target, newTarget) {
			m.desired = newTarget
			m.pivotPending = false
			return false
		}
		// If desired target is unchanged, this request is a no-op.
		if eq(m.desired, newTarget) {
			return false
		}
	}

	m.desired = newTarget
	m.pivotPending = true
	if m.cur.cancel != nil {
		m.cur.cancel(ErrPivotRequested)
	}
	return true
}

// RestartIfPending transitions to a new session if a pivot is pending.
func (m *Manager[T]) RestartIfPending(parent context.Context) (Session[T], context.Context, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.pivotPending {
		return Session[T]{}, nil, false
	}
	s, ctx := m.startLocked(parent)
	return s, ctx, true
}

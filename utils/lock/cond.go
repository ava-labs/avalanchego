// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lock

import (
	"context"
	"sync"
)

// Cond implements a condition variable. Typically [sync.Cond] should be
// preferred. However, this condition variable implementation supports
// cancellable waits.
type Cond struct {
	L sync.Locker

	m sync.Mutex // protects the waiters map
	w map[chan struct{}]struct{}
}

// NewCond returns a new Cond with Locker l.
func NewCond(l sync.Locker) *Cond {
	return &Cond{
		L: l,
		w: make(map[chan struct{}]struct{}),
	}
}

// Wait atomically unlocks c.L and suspends execution of the calling goroutine.
// After later resuming execution, Wait locks c.L before returning. Unlike in
// other systems, Wait cannot return unless awoken by [cond.Broadcast],
// [cond.Signal], or due to the context being cancelled.
//
// Because c.L is not locked while Wait is waiting, the caller typically cannot
// assume that the condition is true when Wait returns, even if the returned
// value is nil. Instead, the caller should Wait in a loop:
//
//	c.L.Lock()
//	defer c.L.Unlock()
//
//	for !condition() {
//		if err := c.Wait(ctx); err != nil {
//			return err
//		}
//	}
//	... make use of condition ...
func (c *Cond) Wait(ctx context.Context) error {
	// Add this thread as a new waiter
	c.m.Lock()
	newL := make(chan struct{})
	c.w[newL] = struct{}{}
	c.m.Unlock()

	c.L.Unlock()
	// We must hold the lock when we return to ensure that the caller can
	// release the lock after wait returns. This is true regardless of if the
	// wait was cancelled or not.
	defer c.L.Lock()

	select {
	case <-ctx.Done():
		// Since the wait was cancelled, we remove our waiting channel on a
		// best-effort basis.
		c.m.Lock()
		delete(c.w, newL)
		c.m.Unlock()

		return ctx.Err()
	case <-newL:
		return nil
	}
}

// Signal wakes one goroutine waiting on c, if there is any.
//
// It is allowed but not required for the caller to hold c.L
// during the call.
//
// Signal() does not affect goroutine scheduling priority; if other goroutines
// are attempting to lock c.L, they may be awoken before a "waiting" goroutine.
func (c *Cond) Signal() {
	c.m.Lock()
	defer c.m.Unlock()

	for w := range c.w {
		close(w)
		delete(c.w, w)
		break
	}
}

// Broadcast wakes all goroutines waiting on c.
//
// It is allowed but not required for the caller to hold c.L
// during the call.
func (c *Cond) Broadcast() {
	c.m.Lock()
	defer c.m.Unlock()

	for w := range c.w {
		close(w)
		delete(c.w, w)
	}
}

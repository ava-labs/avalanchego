// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

import "sync"

// databaseKeepAliveHandle is added to types that hold a lease on the database
// to ensure it is not closed while those types are still in use.
//
// This is necessary to prevent use-after-free bugs where a type holding a
// reference to the database outlives the database itself. Even attempting to
// free those objects after the database has been closed will lead to undefined
// behavior, as a part of the underling Rust object will have already been freed.
type databaseKeepAliveHandle struct {
	mu sync.Mutex
	// [Database.Close] blocks on this WaitGroup, which is set and incremented
	// by [newKeepAliveHandle], and decremented by
	// [databaseKeepAliveHandle.disown].
	outstandingHandles *sync.WaitGroup
}

// init initializes the keep-alive handle to track a new outstanding handle.
func (h *databaseKeepAliveHandle) init(wg *sync.WaitGroup) {
	// lock not necessary today, but will be necessary in the future for types
	// that initialize the handle at some point after construction (#1429).
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.outstandingHandles != nil {
		// setting the finalizer twice will also panic, so we're panicking
		// early to provide better context
		panic("keep-alive handle already initialized")
	}

	h.outstandingHandles = wg
	h.outstandingHandles.Add(1)
}

// disown indicates that the object owning this handle is no longer keeping the
// database alive. If [attemptDisown] returns an error, disowning will only occur
// if [disownEvenOnErr] is true.
//
// This method is safe to call multiple times; subsequent calls after the first
// will continue to invoke [attemptDisown] but will not decrement the wait group
// unless [databaseKeepAliveHandle.init] was called again in the meantime.
func (h *databaseKeepAliveHandle) disown(disownEvenOnErr bool, attemptDisown func() error) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	err := attemptDisown()

	if (err == nil || disownEvenOnErr) && h.outstandingHandles != nil {
		h.outstandingHandles.Done()
		// prevent calling `Done` multiple times if disown is called again, which
		// may happen when the finalizer runs after an explicit call to Drop or Commit.
		h.outstandingHandles = nil
	}

	return err
}

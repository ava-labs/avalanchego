// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"fmt"
	"sync"
)

type handle[T any] struct {
	// handle is an opaque pointer to the underlying Rust object. It should be
	// passed to the C FFI functions that operate on this type.
	//
	// It is not safe to call these methods with a nil handle.
	//
	// Calls to `C.fwd_free_X` will invalidate this handle, so it should not be
	// used after those calls.
	ptr     T
	dropped bool

	// keepAliveHandle is used to keep the database alive while this object is
	// in use. It is initialized when the object is created and disowned after
	// [X.Close] is called.
	keepAliveHandle databaseKeepAliveHandle

	free func(T) C.VoidResult
}

func createHandle[T any](ptr T, wg *sync.WaitGroup, free func(T) C.VoidResult) *handle[T] {
	h := &handle[T]{
		ptr:     ptr,
		free:    free,
		dropped: false,
	}
	h.keepAliveHandle.init(wg)
	return h
}

func drop[T any](h *handle[T]) {
	_ = h.Drop()
}

func (h *handle[T]) Drop() error {
	return h.keepAliveHandle.disown(false /* evenOnError */, func() error {
		if h.dropped {
			return nil
		}

		if err := getErrorFromVoidResult(h.free(h.ptr)); err != nil {
			return fmt.Errorf("%w: %w", errFreeingValue, err)
		}

		// Prevent double free
		var zero T
		h.ptr = zero
		h.dropped = true

		return nil
	})
}

// databaseKeepAliveHandle is added to types that hold a lease on the database
// to ensure it is not closed while those types are still in use.
//
// This is necessary to prevent use-after-free bugs where a type holding a
// reference to the database outlives the database itself. Even attempting to
// free those objects after the database has been closed will lead to undefined
// behavior, as a part of the underling Rust object will have already been freed.
type databaseKeepAliveHandle struct {
	mu sync.RWMutex
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

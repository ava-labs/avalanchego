// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// Package ffi provides a Go wrapper around the [Firewood] database.
//
// [Firewood]: https://github.com/ava-labs/firewood
package ffi

// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

var (
	errRevisionNotFound = errors.New("revision not found")
	errDroppedRevision  = errors.New("revision already dropped")
)

// Revision is an immutable view over the database at a specific root hash.
// Instances are created via Database.Revision, provide read-only access to the revision,
// and must be released with Drop when no longer needed.
type Revision struct {
	// The database this revision is associated with. Holding this ensures
	// the DB outlives the revision for cleanup ordering.
	handle *C.RevisionHandle
	disown sync.Mutex
	// The revision root
	root Hash
	// outstandingHandles is the WaitGroup in Database that tracks open Rust handles.
	// This is incremented when the revision is created, and decremented when Drop is called.
	outstandingHandles *sync.WaitGroup
}

// Get reads the value stored at the provided key within the revision.
//
// It returns errDroppedRevision if Drop has already been called and the underlying
// handle is no longer available.
func (r *Revision) Get(key []byte) ([]byte, error) {
	if r.handle == nil {
		return nil, errDroppedRevision
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	return getValueFromValueResult(C.fwd_get_from_revision(
		r.handle,
		newBorrowedBytes(key, &pinner),
	))
}

// Iter creates an iterator starting from the provided key on revision.
// pass empty slice to start from beginning
func (r *Revision) Iter(key []byte) (*Iterator, error) {
	if r.handle == nil {
		return nil, errDroppedRevision
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	itResult := C.fwd_iter_on_revision(r.handle, newBorrowedBytes(key, &pinner))

	return getIteratorFromIteratorResult(itResult)
}

// Drop releases the resources backed by the revision handle.
//
// It is safe to call Drop multiple times; subsequent calls after the first are no-ops.
func (r *Revision) Drop() error {
	r.disown.Lock()
	defer r.disown.Unlock()

	if r.handle == nil {
		return nil
	}

	if err := getErrorFromVoidResult(C.fwd_free_revision(r.handle)); err != nil {
		return fmt.Errorf("%w: %w", errFreeingValue, err)
	}

	r.outstandingHandles.Done()
	r.handle = nil // Prevent double free

	return nil
}

func (r *Revision) Root() Hash {
	return r.root
}

// getRevisionFromResult converts a C.RevisionResult to a Revision or error.
func getRevisionFromResult(result C.RevisionResult, openProposals *sync.WaitGroup) (*Revision, error) {
	switch result.tag {
	case C.RevisionResult_NullHandlePointer:
		return nil, errDBClosed
	case C.RevisionResult_RevisionNotFound:
		return nil, errRevisionNotFound
	case C.RevisionResult_Ok:
		body := (*C.RevisionResult_Ok_Body)(unsafe.Pointer(&result.anon0))
		hashKey := *(*Hash)(unsafe.Pointer(&body.root_hash._0))
		rev := &Revision{
			handle:             body.handle,
			root:               hashKey,
			outstandingHandles: openProposals,
		}
		openProposals.Add(1)
		runtime.SetFinalizer(rev, (*Revision).Drop)
		return rev, nil
	case C.RevisionResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.RevisionResult tag: %d", result.tag)
	}
}

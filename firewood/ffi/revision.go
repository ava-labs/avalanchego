// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

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
	ErrDroppedRevision  = errors.New("revision already dropped")
	errRevisionNotFound = errors.New("revision not found")
)

// Revision is an immutable view over the state at a specific root hash.
// Instances are created via [Database.Revision], provide read-only access to
// the revision, and must be released with [Revision.Drop] when no longer needed.
//
// Revisions must be dropped before the associated database is closed. A finalizer
// is set on each Revision to ensure that Drop is called when the Revision is
// garbage collected, but relying on finalizers is not recommended. Failing to
// drop a revision before the database is closed will cause it to block or fail.
//
// Additionally, Revisions should be dropped when no longer needed to allow the
// database to free any associated resources. Firewood ensures that the state
// associated with a Revision is retained until all Revisions based on that state
// have been dropped.
//
// All operations on a Revision are thread-safe with respect to each other.
type Revision struct {
	// handle is an opaque pointer to the revision within Firewood. It should be
	// passed to the C FFI functions that operate on revisions
	//
	// It is not safe to call these methods with a nil handle.
	//
	// Calls to `C.fwd_free_revision` will invalidate this handle, so it should
	// not be used after that call.
	handle *C.RevisionHandle

	// root is the root hash of the revision.
	root Hash

	// keepAliveHandle is used to keep the database alive while this revision is
	// in use. It is initialized when the revision is created and disowned after
	// [Revision.Drop] is called.
	keepAliveHandle databaseKeepAliveHandle
}

// Get reads the value stored at the provided key within the revision.
// If the key does not exist, it returns nil.
//
// It returns ErrDroppedRevision if Drop has already been called.
func (r *Revision) Get(key []byte) ([]byte, error) {
	if r.handle == nil {
		return nil, ErrDroppedRevision
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
// It returns ErrDroppedRevision if Drop has already been called.
func (r *Revision) Iter(key []byte) (*Iterator, error) {
	if r.handle == nil {
		return nil, ErrDroppedRevision
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
	return r.keepAliveHandle.disown(false /* evenOnError */, func() error {
		if r.handle == nil {
			return nil
		}

		if err := getErrorFromVoidResult(C.fwd_free_revision(r.handle)); err != nil {
			return fmt.Errorf("%w: %w", errFreeingValue, err)
		}

		r.handle = nil
		return nil
	})
}

// Root returns the root hash of the revision.
func (r *Revision) Root() Hash {
	return r.root
}

// getRevisionFromResult converts a C.RevisionResult to a Revision or error.
func getRevisionFromResult(result C.RevisionResult, wg *sync.WaitGroup) (*Revision, error) {
	switch result.tag {
	case C.RevisionResult_NullHandlePointer:
		return nil, errDBClosed
	case C.RevisionResult_RevisionNotFound:
		return nil, errRevisionNotFound
	case C.RevisionResult_Ok:
		body := (*C.RevisionResult_Ok_Body)(unsafe.Pointer(&result.anon0))
		hashKey := *(*Hash)(unsafe.Pointer(&body.root_hash._0))
		rev := &Revision{
			handle: body.handle,
			root:   hashKey,
		}
		rev.keepAliveHandle.init(wg)
		runtime.SetFinalizer(rev, (*Revision).Drop)
		return rev, nil
	case C.RevisionResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.RevisionResult tag: %d", result.tag)
	}
}

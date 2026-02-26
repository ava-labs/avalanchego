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
	ErrDroppedRevision       = errors.New("revision already dropped")
	errRevisionNotFound      = errors.New("revision not found")
	ErrEndRevisionNotFound   = errors.New("end revision not found")
	ErrStartRevisionNotFound = errors.New("start revision not found")
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
	*handle[*C.RevisionHandle]

	// root is the root hash of the revision.
	root Hash
}

// Get reads the value stored at the provided key within the revision.
// If the key does not exist, it returns nil.
//
// It returns ErrDroppedRevision if Drop has already been called.
func (r *Revision) Get(key []byte) ([]byte, error) {
	r.keepAliveHandle.mu.RLock()
	defer r.keepAliveHandle.mu.RUnlock()
	if r.dropped {
		return nil, ErrDroppedRevision
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	return getValueFromValueResult(C.fwd_get_from_revision(
		r.ptr,
		newBorrowedBytes(key, &pinner),
	))
}

// Iter creates an [Iterator] over the key-value pairs in this revision,
// starting at the first key greater than or equal to the provided key.
// Pass nil or an empty slice to iterate from the beginning.
//
// The Iterator must be released with [Iterator.Drop] when no longer needed.
//
// It returns [ErrDroppedRevision] if Drop has already been called.
func (r *Revision) Iter(key []byte) (*Iterator, error) {
	r.keepAliveHandle.mu.RLock()
	defer r.keepAliveHandle.mu.RUnlock()
	if r.dropped {
		return nil, ErrDroppedRevision
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	itResult := C.fwd_iter_on_revision(r.ptr, newBorrowedBytes(key, &pinner))

	return getIteratorFromIteratorResult(itResult)
}

// Root returns the root hash of the revision.
func (r *Revision) Root() Hash {
	return r.root
}

// Dump returns a DOT (Graphviz) format representation of the trie structure
// of this revision for debugging purposes.
//
// Returns ErrDroppedRevision if Drop has already been called.
func (r *Revision) Dump() (string, error) {
	r.keepAliveHandle.mu.RLock()
	defer r.keepAliveHandle.mu.RUnlock()
	if r.dropped {
		return "", ErrDroppedRevision
	}

	bytes, err := getValueFromValueResult(C.fwd_revision_dump(r.ptr))
	if err != nil {
		return "", err
	}

	return string(bytes), nil
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
			handle: createHandle(body.handle, wg, func(r *C.RevisionHandle) C.VoidResult { return C.fwd_free_revision(r) }),
			root:   hashKey,
		}
		runtime.AddCleanup(rev, drop, rev.handle)
		return rev, nil
	case C.RevisionResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.RevisionResult tag: %d", result.tag)
	}
}

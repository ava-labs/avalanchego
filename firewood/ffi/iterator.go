// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

// #include <stdlib.h>
// #include "firewood.h"
// #cgo noescape fwd_iter_next
// #cgo nocallback fwd_iter_next
// #cgo noescape fwd_iter_next_n
// #cgo nocallback fwd_iter_next_n
// #cgo noescape fwd_free_iterator
// #cgo nocallback fwd_free_iterator
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

// iteratorHandle extends [handle] with iterator-specific state — the
// currently-borrowed FFI batch/KV — so that Drop can release that resource
// without needing a closure that captures the outer *Iterator wrapper.
//
// Capturing the wrapper would have a subtle but real cost: the keep-alive
// registry stores the registered dropFn for the lifetime of the handle, so
// a wrapper-bound closure would keep the wrapper reachable for GC, and
// [runtime.AddCleanup] would never fire as a back-stop for users who
// forget to Drop. With the iterator-specific state owned by this inner
// type, the registry's dropFn is bound to *iteratorHandle, the wrapper is
// independently reclaimable, and the cleanup path works as advertised.

// freer is the contract for an FFI-owned pair or batch returned by
// fwd_iter_next / fwd_iter_next_n: a single free() that releases the
// underlying Rust allocation. The concrete types are [ownedKeyValue]
// and [ownedKeyValueBatch], both unexported in the ffi package.
type freer interface {
	free() error
}

type iteratorHandle struct {
	handle[*C.IteratorHandle]

	// currentResource is the FFI-owned pair or batch most recently returned
	// from fwd_iter_next / fwd_iter_next_n. It must be freed before the
	// next FFI call (so a borrowed batch isn't invalidated mid-iteration)
	// and as part of Drop. Mutated by [Iterator.nextInternal] under
	// lease.mu.RLock; read and cleared by Drop under
	// lease.mu.Lock.
	currentResource freer
}

func (ih *iteratorHandle) freeCurrentAllocation() error {
	if ih.currentResource == nil {
		return nil
	}
	e := ih.currentResource.free()
	ih.currentResource = nil
	return e
}

// Drop releases the resources associated with the iterator. This must be
// called when the iterator is no longer needed to avoid memory leaks. As a
// safety net, [runtime.AddCleanup] also drops the iterator if its wrapper
// becomes unreachable without an explicit Drop.
//
// It is safe to call Drop multiple times and from multiple goroutines;
// subsequent calls after the first are no-ops.
func (ih *iteratorHandle) Drop() error {
	// Disowning is unconditional — see handle[T].Drop for the reasoning
	// behind that choice. (Not in the godoc: this type is embedded in the
	// exported Iterator, and links to unexported types don't render.)
	return ih.lease.release(func() error {
		err := ih.freeCurrentAllocation()
		if ih.dropped {
			return err
		}
		// Mark dropped before freeing so a panic in free cannot leave a
		// retry able to double-free ptr.
		ptr := ih.ptr
		ih.ptr = nil
		ih.dropped = true
		if e := getErrorFromVoidResult(ih.free(ptr)); e != nil {
			err = errors.Join(err, fmt.Errorf("%w: %w", errFreeingValue, e))
		}
		return err
	})
}

// iteratorCleanup is the [runtime.AddCleanup] callback. It must be a
// top-level function (not a closure capturing the wrapper) so that the
// runtime's "cleanup must not refer to ptr" invariant holds trivially.
func iteratorCleanup(ih *iteratorHandle) {
	_ = ih.Drop()
}

// Iterator provides sequential access to key-value pairs within a [Revision] or [Proposal].
// An iterator traverses the trie in lexicographic key order, starting from a specified key.
//
// Instances are created via [Revision.Iter] or [Proposal.Iter], and must be released
// with [Iterator.Drop] when no longer needed.
//
// An Iterator holds a reference to the underlying view, so it can safely outlive the
// Revision or Proposal it was created from. The underlying state will not be released
// until the Iterator is released. The Iterator additionally keeps the [Database]
// alive: [Database.Close] will block on outstanding iterators (or, with
// [WithForceCloseHandles], drop them).
//
// Iterator supports two modes of accessing key-value pairs. [Iterator.Next] copies
// the key and value into Go-managed memory. [Iterator.NextBorrowed] returns slices
// that borrow Rust-owned memory, which is faster but the slices are only valid until
// the next call to Next, NextBorrowed, or [Iterator.Drop].
//
// An Iterator is single-reader: Next, NextBorrowed, Key, Value, and Err must
// not be called concurrently. Drop is the exception and is safe from another
// goroutine, including force-close.
type Iterator struct {
	// iteratorHandle owns the Rust iterator pointer, the keep-alive handle on
	// the parent database, and the currently-borrowed FFI batch/KV. Calls
	// to fwd_free_iterator (via iteratorHandle.Drop) will invalidate ptr.
	*iteratorHandle

	// batchSize is the number of items that are loaded at once
	// to reduce ffi call overheads
	batchSize int

	// loadedPairs is the latest loaded key value pairs retrieved
	// from the iterator, not yet consumed by user
	loadedPairs []*ownedKeyValue

	// current* fields correspond to the current cursor state
	// nil/empty if not started or exhausted; refreshed on each Next().
	currentPair  *ownedKeyValue
	currentKey   []byte
	currentValue []byte

	// err is the error from the iterator, if any
	err error
}

func (it *Iterator) nextInternal() error {
	if len(it.loadedPairs) > 0 {
		it.currentPair, it.loadedPairs = it.loadedPairs[0], it.loadedPairs[1:]
		return nil
	}

	// current resources should **only** be freed, on the next call to the FFI
	// this is to make sure we don't invalidate a batch in between iteration
	if e := it.freeCurrentAllocation(); e != nil {
		return e
	}
	if it.batchSize <= 1 {
		kv, e := getKeyValueFromResult(C.fwd_iter_next(it.ptr))
		if e != nil {
			return e
		}
		it.currentPair = kv
		it.currentResource = kv
	} else {
		batch, e := getKeyValueBatchFromResult(C.fwd_iter_next_n(it.ptr, C.size_t(it.batchSize)))
		if e != nil {
			return e
		}
		pairs := batch.copy()
		if len(pairs) > 0 {
			it.currentPair, it.loadedPairs = pairs[0], pairs[1:]
		} else {
			it.currentPair = nil
		}
		it.currentResource = batch
	}

	return nil
}

// SetBatchSize sets the number of key-value pairs to retrieve per FFI call.
// A batch size greater than 1 reduces FFI overhead when iterating over many items.
// A batch size of 0 or 1 disables batching. The default is 0.
func (it *Iterator) SetBatchSize(batchSize int) {
	it.batchSize = batchSize
}

// Next advances the iterator to the next key-value pair and returns true if
// a pair is available. The key and value can be retrieved with [Iterator.Key]
// and [Iterator.Value].
//
// Next copies the key and value into Go-managed memory, making them safe to
// retain after subsequent calls. For better performance, use [Iterator.NextBorrowed].
//
// It returns false when the iterator is exhausted or an error occurs. Check
// [Iterator.Err] after iteration completes to distinguish between the two.
// It is safe to call Next after it returns false; it will continue to return false.
func (it *Iterator) Next() bool {
	it.lease.mu.RLock()
	defer it.lease.mu.RUnlock()
	if it.dropped {
		it.err = errDroppedIterator
		return false
	}
	it.err = it.nextInternal()
	if it.currentPair == nil || it.err != nil {
		return false
	}
	k, v := it.currentPair.copy()
	it.currentKey = k
	it.currentValue = v
	return true
}

// NextBorrowed advances the iterator like [Iterator.Next], but the slices returned
// by [Iterator.Key] and [Iterator.Value] borrow Rust-owned memory instead of copying.
// This is faster than Next but the slices are only valid until the next call to
// [Iterator.Next], [Iterator.NextBorrowed], or [Iterator.Drop].
//
// WARNING: Do not retain, store, or modify the slices. Doing so results in undefined behavior.
//
// It returns false when the iterator is exhausted or an error occurs, same as Next.
func (it *Iterator) NextBorrowed() bool {
	it.lease.mu.RLock()
	defer it.lease.mu.RUnlock()
	if it.dropped {
		it.err = errDroppedIterator
		return false
	}
	it.err = it.nextInternal()
	if it.currentPair == nil || it.err != nil {
		return false
	}
	it.currentKey = it.currentPair.key.BorrowedBytes()
	it.currentValue = it.currentPair.value.BorrowedBytes()
	return true
}

// Key returns the key of the current key-value pair.
// If the iterator has not been advanced or is exhausted, it returns nil.
//
// If the iterator was advanced with [Iterator.NextBorrowed], the returned slice
// borrows Rust memory and is only valid until the next call to [Iterator.Next],
// [Iterator.NextBorrowed], or [Iterator.Drop] — including a Drop initiated by
// another goroutine, such as [Database.Close] with [WithForceCloseHandles].
// Key deliberately takes no lock: the caller consumes the slice after Key
// returns, so a lock here could not extend the slice's validity.
func (it *Iterator) Key() []byte {
	if it.currentPair == nil || it.err != nil {
		return nil
	}
	return it.currentKey
}

// Value returns the value of the current key-value pair.
// If the iterator has not been advanced or is exhausted, it returns nil.
//
// If the iterator was advanced with [Iterator.NextBorrowed], the returned slice
// borrows Rust memory and is only valid until the next call to [Iterator.Next],
// [Iterator.NextBorrowed], or [Iterator.Drop] — including a Drop initiated by
// another goroutine, such as [Database.Close] with [WithForceCloseHandles].
// Value deliberately takes no lock: the caller consumes the slice after Value
// returns, so a lock here could not extend the slice's validity.
func (it *Iterator) Value() []byte {
	if it.currentPair == nil || it.err != nil {
		return nil
	}
	return it.currentValue
}

// Err returns the error from the last call to [Iterator.Next] or [Iterator.NextBorrowed],
// or nil if no error occurred.
func (it *Iterator) Err() error {
	return it.err
}

// errDroppedIterator wraps [ErrDropped].
var errDroppedIterator = fmt.Errorf("iterator %w", ErrDropped)

// getIteratorFromIteratorResult converts a C.IteratorResult to an Iterator or error.
func getIteratorFromIteratorResult(result C.IteratorResult, registry *keepAliveRegistry) (*Iterator, error) {
	switch result.tag {
	case C.IteratorResult_NullHandlePointer:
		return nil, errDBClosed
	case C.IteratorResult_Ok:
		body := (*C.IteratorResult_Ok_Body)(unsafe.Pointer(&result.anon0))
		ih := &iteratorHandle{
			handle: handle[*C.IteratorHandle]{
				ptr: body.handle,
				free: func(h *C.IteratorHandle) C.VoidResult {
					return C.fwd_free_iterator(h)
				},
			},
		}
		// ih.Drop is promoted from *iteratorHandle, so the registered
		// closure is bound to ih (not the *Iterator wrapper). This is
		// what lets the cleanup below fire when the user drops their
		// last reference to the wrapper without an explicit Drop.
		if err := ih.lease.attach(registry, ih.Drop); err != nil {
			return nil, err
		}
		it := &Iterator{iteratorHandle: ih}
		// Cleanup arg is ih (distinct from it, no back-reference) per
		// AddCleanup's contract; see [iteratorHandle] for the rationale.
		runtime.AddCleanup(it, iteratorCleanup, ih)
		return it, nil
	case C.IteratorResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.IteratorResult tag: %d", result.tag)
	}
}

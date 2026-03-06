// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// Iterator provides sequential access to key-value pairs within a [Revision] or [Proposal].
// An iterator traverses the trie in lexicographic key order, starting from a specified key.
//
// Instances are created via [Revision.Iter] or [Proposal.Iter], and must be released
// with [Iterator.Drop] when no longer needed.
//
// An Iterator holds a reference to the underlying view, so it can safely outlive the
// Revision or Proposal it was created from. The underlying state will not be released
// until the Iterator is dropped.
//
// Iterator supports two modes of accessing key-value pairs. [Iterator.Next] copies
// the key and value into Go-managed memory. [Iterator.NextBorrowed] returns slices
// that borrow Rust-owned memory, which is faster but the slices are only valid until
// the next call to Next, NextBorrowed, or Drop.
type Iterator struct {
	// handle is an opaque pointer to the iterator within Firewood. It should be
	// passed to the C FFI functions that operate on iterators
	//
	// It is not safe to call these methods with a nil handle.
	handle *C.IteratorHandle

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
	// FFI resource for current pair or batch to free on advance or drop
	currentResource interface{ free() error }

	// err is the error from the iterator, if any
	err error
}

func (it *Iterator) freeCurrentAllocation() error {
	if it.currentResource == nil {
		return nil
	}
	e := it.currentResource.free()
	it.currentResource = nil
	return e
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
		kv, e := getKeyValueFromResult(C.fwd_iter_next(it.handle))
		if e != nil {
			return e
		}
		it.currentPair = kv
		it.currentResource = kv
	} else {
		batch, e := getKeyValueBatchFromResult(C.fwd_iter_next_n(it.handle, C.size_t(it.batchSize)))
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
	it.err = it.nextInternal()
	if it.currentPair == nil || it.err != nil {
		return false
	}
	it.currentKey = it.currentPair.key.BorrowedBytes()
	it.currentValue = it.currentPair.value.BorrowedBytes()
	it.err = nil
	return true
}

// Key returns the key of the current key-value pair.
// If the iterator has not been advanced or is exhausted, it returns nil.
//
// If the iterator was advanced with [Iterator.NextBorrowed], the returned slice
// borrows Rust memory and is only valid until the next advance or drop.
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
// borrows Rust memory and is only valid until the next advance or drop.
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

// Drop releases the resources associated with the iterator. This must be called
// when the iterator is no longer needed to avoid memory leaks.
//
// It is safe to call Drop multiple times; subsequent calls after the first are no-ops.
func (it *Iterator) Drop() error {
	err := it.freeCurrentAllocation()
	if it.handle != nil {
		// Always free the iterator even if releasing the current KV/batch failed.
		// The iterator holds a NodeStore ref that must be dropped.
		err = errors.Join(
			err,
			getErrorFromVoidResult(C.fwd_free_iterator(it.handle)))
		// prevent use-after-free/double-free
		it.handle = nil
	}
	return err
}

// getIteratorFromIteratorResult converts a C.IteratorResult to an Iterator or error.
func getIteratorFromIteratorResult(result C.IteratorResult) (*Iterator, error) {
	switch result.tag {
	case C.IteratorResult_NullHandlePointer:
		return nil, errDBClosed
	case C.IteratorResult_Ok:
		body := (*C.IteratorResult_Ok_Body)(unsafe.Pointer(&result.anon0))
		proposal := &Iterator{
			handle: body.handle,
		}
		return proposal, nil
	case C.IteratorResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.IteratorResult tag: %d", result.tag)
	}
}

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

// SetBatchSize sets the max number of pairs to be retrieved in one ffi call.
func (it *Iterator) SetBatchSize(batchSize int) {
	it.batchSize = batchSize
}

// Next proceeds to the next item on the iterator, and returns true
// if succeeded and there is a pair available.
// The new pair could be retrieved with Key and Value methods.
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

// NextBorrowed is like Next, but Key and Value **borrow** rust-owned buffers.
//
// Lifetime: the returned slices are valid **only until** the next call to
// Next, NextBorrowed, Close, or any operation that advances/invalidates the iterator.
// They alias FFI-owned memory that will be **freed or reused** on the next advance.
//
// Do **not** retain, store, or modify these slices.
// **Copy** or use Next if you need to keep them.
// Misuse can read freed memory and cause undefined behavior.
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

// Key returns the key of the current pair
func (it *Iterator) Key() []byte {
	if it.currentPair == nil || it.err != nil {
		return nil
	}
	return it.currentKey
}

// Value returns the value of the current pair
func (it *Iterator) Value() []byte {
	if it.currentPair == nil || it.err != nil {
		return nil
	}
	return it.currentValue
}

// Err returns the error if Next failed
func (it *Iterator) Err() error {
	return it.err
}

// Drop drops the iterator and releases the resources
func (it *Iterator) Drop() error {
	err := it.freeCurrentAllocation()
	if it.handle != nil {
		// Always free the iterator even if releasing the current KV/batch failed.
		// The iterator holds a NodeStore ref that must be dropped.
		return errors.Join(
			err,
			getErrorFromVoidResult(C.fwd_free_iterator(it.handle)))
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

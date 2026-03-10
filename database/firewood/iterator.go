//go:build cgo && !windows
// +build cgo,!windows

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"bytes"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/firewood-go-ethhash/ffi"
)

// pendingKV represents a key-value operation (put or delete) in pending batch
type pendingKV struct {
	key    []byte
	value  []byte
	delete bool
}

// nativeIterator implements database.Iterator using Firewood's native FFI trie iterator.
//
// Architecture:
// - Uses Firewood's Revision.Iter() to iterate directly over the persisted trie
// - Merges with pending (uncommitted) operations for read-your-writes consistency
// - Works correctly after restart because it reads from the trie, not from a registry
//
// Merge algorithm:
// - FFI iterator provides sorted key-value pairs from the committed trie
// - Pending operations are pre-sorted and pre-filtered by prefix/start
// - FFI keys that have ANY pending operation (put or delete) are suppressed
// - Only pending put operations are yielded (deletes suppress FFI keys only)
// - Standard sorted merge of the two clean streams produces correct output
type nativeIterator struct {
	// FFI resources (nil if database is empty or Revision unavailable)
	ffiIter  *ffi.Iterator
	revision *ffi.Revision

	// FFI cursor state
	ffiKey   []byte
	ffiValue []byte
	ffiDone  bool

	// Pending operations cursor (sorted, filtered by prefix/start)
	pending    []pendingKV
	pendingIdx int // Index of current pending entry (-1 = before start)

	// Set of ALL pending keys (puts + deletes) for suppressing FFI duplicates
	pendingKeys map[string]bool

	// Prefix filter (empty = no filter)
	prefix []byte

	// Current output
	currentKey   []byte
	currentValue []byte

	// State
	err      error
	released bool
	started  bool
}

// newNativeIterator creates an iterator using Firewood's native FFI trie iterator.
// Returns a database.Iterator that merges committed trie data with pending operations.
//
// If the database is empty or the Revision is unavailable, falls back to iterating
// only pending operations (graceful degradation).
func newNativeIterator(
	fw *ffi.Database,
	currentRoot ffi.Hash,
	pending []pendingKV,
	startKey []byte,
	prefix []byte,
) database.Iterator {
	// CRITICAL: Make defensive copies of prefix and startKey.
	// PrefixDB passes byte slices from its buffer pool, then returns the buffers
	// to the pool via defer after this function returns. Without copies, the
	// iterator's prefix field would reference pool memory that gets overwritten
	// by subsequent PrefixDB operations, causing premature iterator termination.
	var prefixCopy []byte
	if len(prefix) > 0 {
		prefixCopy = make([]byte, len(prefix))
		copy(prefixCopy, prefix)
	}
	var startKeyCopy []byte
	if len(startKey) > 0 {
		startKeyCopy = make([]byte, len(startKey))
		copy(startKeyCopy, startKey)
	}

	// Build the set of ALL pending keys for FFI suppression
	pendingKeys := make(map[string]bool, len(pending))
	for _, op := range pending {
		pendingKeys[string(op.key)] = true
	}

	it := &nativeIterator{
		ffiDone:     true, // Will be set to false if FFI iterator created successfully
		pending:     pending,
		pendingIdx:  -1,
		pendingKeys: pendingKeys,
		prefix:      prefixCopy,
	}

	// Empty database: iterate only pending ops
	if currentRoot == ffi.EmptyRoot {
		return it
	}

	// Get a revision handle for the current root.
	// This reads from the persisted trie, so it works after restart.
	revision, err := fw.Revision(currentRoot)
	if err != nil {
		// Revision unavailable - iterate only pending ops (graceful degradation)
		return it
	}

	// Determine FFI iterator start position.
	// Use the greater of startKey and prefix as the starting point.
	iterStart := startKeyCopy
	if len(prefixCopy) > 0 && (len(iterStart) == 0 || bytes.Compare(prefixCopy, iterStart) > 0) {
		iterStart = prefixCopy
	}
	if iterStart == nil {
		iterStart = []byte{} // FFI expects non-nil slice for "start from beginning"
	}

	ffiIter, err := revision.Iter(iterStart)
	if err != nil {
		revision.Drop()
		return it
	}

	// Batch loading reduces FFI call overhead during iteration
	ffiIter.SetBatchSize(256)

	it.ffiIter = ffiIter
	it.revision = revision
	it.ffiDone = false

	return it
}

// advanceFFI moves the FFI cursor to the next valid key.
// Skips keys outside the prefix range and keys with pending operations.
func (it *nativeIterator) advanceFFI() {
	if it.ffiIter == nil {
		it.ffiDone = true
		return
	}

	for {
		if !it.ffiIter.Next() {
			it.ffiDone = true
			if err := it.ffiIter.Err(); err != nil {
				it.err = err
			}
			return
		}

		key := it.ffiIter.Key()

		// Prefix boundary check
		if len(it.prefix) > 0 {
			if !bytes.HasPrefix(key, it.prefix) {
				if bytes.Compare(key, it.prefix) > 0 {
					// Past the prefix range, iteration is done
					it.ffiDone = true
					return
				}
				continue // Before prefix range (shouldn't happen with proper start)
			}
		}

		// Skip keys that have pending operations (pending always takes precedence)
		if it.pendingKeys[string(key)] {
			continue
		}

		// Valid key: copy data (FFI memory may be reused on next advance)
		it.ffiKey = make([]byte, len(key))
		copy(it.ffiKey, key)
		val := it.ffiIter.Value()
		it.ffiValue = make([]byte, len(val))
		copy(it.ffiValue, val)
		return
	}
}

// advancePending moves the pending cursor to the next non-delete entry.
// Delete operations are only used for suppressing FFI keys (via pendingKeys).
func (it *nativeIterator) advancePending() {
	for {
		it.pendingIdx++
		if it.pendingIdx >= len(it.pending) {
			return
		}
		if !it.pending[it.pendingIdx].delete {
			return // Found a put operation to yield
		}
	}
}

// pendingCurrent returns the current pending entry, or nil if exhausted.
func (it *nativeIterator) pendingCurrent() *pendingKV {
	if it.pendingIdx >= 0 && it.pendingIdx < len(it.pending) {
		return &it.pending[it.pendingIdx]
	}
	return nil
}

// Next implements database.Iterator
func (it *nativeIterator) Next() bool {
	if it.released {
		it.err = database.ErrClosed
		return false
	}
	if it.err != nil {
		return false
	}

	// First call: prime both cursors
	if !it.started {
		it.started = true
		it.advanceFFI()
		it.advancePending()
	}

	hasFfi := !it.ffiDone
	pCur := it.pendingCurrent()

	if !hasFfi && pCur == nil {
		return false
	}

	if hasFfi && pCur == nil {
		// Only FFI data remains
		it.currentKey = it.ffiKey
		it.currentValue = it.ffiValue
		it.advanceFFI()
		return true
	}

	if !hasFfi {
		// Only pending data remains
		it.currentKey = pCur.key
		it.currentValue = pCur.value
		it.advancePending()
		return true
	}

	// Both have data: standard sorted merge - pick the smaller key
	cmp := bytes.Compare(it.ffiKey, pCur.key)
	if cmp < 0 {
		it.currentKey = it.ffiKey
		it.currentValue = it.ffiValue
		it.advanceFFI()
	} else if cmp > 0 {
		it.currentKey = pCur.key
		it.currentValue = pCur.value
		it.advancePending()
	} else {
		// Same key: pending wins, advance both cursors
		it.currentKey = pCur.key
		it.currentValue = pCur.value
		it.advanceFFI()
		it.advancePending()
	}

	return true
}

// Error implements database.Iterator
func (it *nativeIterator) Error() error {
	return it.err
}

// Key implements database.Iterator
func (it *nativeIterator) Key() []byte {
	if it.released || it.err != nil {
		return nil
	}
	return it.currentKey
}

// Value implements database.Iterator
func (it *nativeIterator) Value() []byte {
	if it.released || it.err != nil {
		return nil
	}
	return it.currentValue
}

// Release implements database.Iterator
func (it *nativeIterator) Release() {
	if it.released {
		return
	}
	it.released = true

	// Release FFI resources
	if it.ffiIter != nil {
		it.ffiIter.Drop()
		it.ffiIter = nil
	}
	if it.revision != nil {
		it.revision.Drop()
		it.revision = nil
	}

	it.currentKey = nil
	it.currentValue = nil
	it.ffiKey = nil
	it.ffiValue = nil
	it.pending = nil
	it.pendingKeys = nil
}

// errorIterator is a special iterator that always returns an error.
// Used when database is closed or initialization fails.
type errorIterator struct {
	err error
}

func newErrorIterator(err error) database.Iterator {
	return &errorIterator{err: err}
}

func (it *errorIterator) Next() bool    { return false }
func (it *errorIterator) Error() error  { return it.err }
func (it *errorIterator) Key() []byte   { return nil }
func (it *errorIterator) Value() []byte { return nil }
func (it *errorIterator) Release()      {}

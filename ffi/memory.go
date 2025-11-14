// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// Package ffi provides a Go wrapper around the [Firewood] database.
//
// [Firewood]: https://github.com/ava-labs/firewood
package ffi

// // Note that -lm is required on Linux but not on Mac.
// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

var (
	errKeysAndValues = errors.New("keys and values must have the same length")
	errFreeingValue  = errors.New("unexpected error while freeing value")
)

type Pinner interface {
	Pin(ptr any)
	Unpin()
}

// Borrower is an interface for types that can borrow or copy bytes returned
// from FFI methods.
type Borrower interface {
	// BorrowedBytes returns a slice of bytes that borrows the data from the
	// Borrower's internal memory.
	//
	// The returned slice is valid only as long as the Borrower is valid.
	// If the Borrower is freed, the slice will become invalid.
	BorrowedBytes() []byte

	// CopiedBytes returns a slice of bytes that is a copy of the Borrower's
	// internal memory.
	//
	// This is fully independent of the borrowed data and is valid even after
	// the Borrower is freed.
	CopiedBytes() []byte

	// Free releases the memory associated with the Borrower's data.
	//
	// It is safe to call this method multiple times. Subsequent calls will
	// do nothing if the data has already been freed (or was never set).
	//
	// However, it is not safe to call this method concurrently from multiple
	// goroutines. It is also not safe to call this method while there are
	// outstanding references to the slice returned by BorrowedBytes. Any
	// existing slices will become invalid and may cause undefined behavior
	// if used after the Free call.
	Free() error
}

var _ Borrower = (*ownedBytes)(nil)

// newBorrowedBytes creates a new BorrowedBytes from a Go byte slice.
//
// Provide a Pinner to ensure the memory is pinned while the BorrowedBytes is in use.
func newBorrowedBytes(slice []byte, pinner Pinner) C.BorrowedBytes {
	// Get the pointer first to distinguish between nil slice and empty slice
	ptr := unsafe.SliceData(slice)
	sliceLen := len(slice)

	// If ptr is nil (which means the slice itself is nil), return nil pointer
	if ptr == nil {
		return C.BorrowedBytes{ptr: nil, len: 0}
	}

	// For non-nil slices (including empty slices like []byte{}),
	// pin the pointer if the slice has data
	if sliceLen > 0 {
		pinner.Pin(ptr)
	}

	return C.BorrowedBytes{
		ptr: (*C.uint8_t)(ptr),
		len: C.size_t(sliceLen),
	}
}

// newKeyValuePair creates a new KeyValuePair from Go byte slices for key and value.
//
// Provide a Pinner to ensure the memory is pinned while the KeyValuePair is in use.
func newKeyValuePair(key, value []byte, pinner Pinner) C.KeyValuePair {
	return C.KeyValuePair{
		key:   newBorrowedBytes(key, pinner),
		value: newBorrowedBytes(value, pinner),
	}
}

// newBorrowedKeyValuePairs creates a new BorrowedKeyValuePairs from a slice of KeyValuePair.
//
// Provide a Pinner to ensure the memory is pinned while the BorrowedKeyValuePairs is
// in use.
func newBorrowedKeyValuePairs(pairs []C.KeyValuePair, pinner Pinner) C.BorrowedKeyValuePairs {
	sliceLen := len(pairs)
	if sliceLen == 0 {
		return C.BorrowedKeyValuePairs{ptr: nil, len: 0}
	}

	ptr := unsafe.SliceData(pairs)
	if ptr == nil {
		return C.BorrowedKeyValuePairs{ptr: nil, len: 0}
	}

	pinner.Pin(ptr)

	return C.BorrowedKeyValuePairs{
		ptr: ptr,
		len: C.size_t(sliceLen),
	}
}

// newKeyValuePairs creates a new BorrowedKeyValuePairs from slices of keys and values.
//
// The keys and values must have the same length.
//
// Provide a Pinner to ensure the memory is pinned while the BorrowedKeyValuePairs is
// in use.
func newKeyValuePairs(keys, vals [][]byte, pinner Pinner) (C.BorrowedKeyValuePairs, error) {
	if len(keys) != len(vals) {
		return C.BorrowedKeyValuePairs{}, fmt.Errorf("%w: %d != %d", errKeysAndValues, len(keys), len(vals))
	}

	pairs := make([]C.KeyValuePair, len(keys))
	for i := range keys {
		pairs[i] = newKeyValuePair(keys[i], vals[i], pinner)
	}

	return newBorrowedKeyValuePairs(pairs, pinner), nil
}

// ownedBytes is a wrapper around C.OwnedBytes that provides a Go interface
// for Rust-owned byte slices.
//
// ownedBytes implements the [Borrower] interface allowing it to be shared
// outside of the FFI package without exposing the C types directly or any FFI
// implementation details.
type ownedBytes struct {
	owned C.OwnedBytes
}

// Free releases the memory associated with the Borrower's data.
//
// It is safe to call this method multiple times. Subsequent calls will
// do nothing if the data has already been freed (or was never set).
//
// However, it is not safe to call this method concurrently from multiple
// goroutines. It is also not safe to call this method while there are
// outstanding references to the slice returned by BorrowedBytes. Any
// existing slices will become invalid and may cause undefined behavior
// if used after the Free call.
func (b *ownedBytes) Free() error {
	if b.owned.ptr == nil {
		// Already freed (or never set), nothing to do.
		return nil
	}

	if err := getErrorFromVoidResult(C.fwd_free_owned_bytes(b.owned)); err != nil {
		return fmt.Errorf("%w: %w", errFreeingValue, err)
	}

	b.owned = C.OwnedBytes{}

	return nil
}

// BorrowedBytes returns the underlying byte slice. It may return nil if the
// data has already been freed was never set.
//
// The returned slice is valid only as long as the ownedBytes is valid.
//
// It does not copy the data; however, the slice is valid only as long as the
// ownedBytes is valid. If the ownedBytes is freed, the slice will
// become invalid.
//
// It is safe to cast the returned slice as a string so long as the ownedBytes
// is not freed while the string is in use.
//
// BorrowedBytes is part of the [Borrower] interface.
func (b *ownedBytes) BorrowedBytes() []byte {
	if b.owned.ptr == nil {
		return nil
	}

	return unsafe.Slice((*byte)(b.owned.ptr), b.owned.len)
}

// CopiedBytes returns a copy of the underlying byte slice. It may return nil if the
// data has already been freed or was never set.
//
// The returned slice is a copy of the data and is valid independently of the
// ownedBytes. It is safe to use after the ownedBytes is freed and will
// be freed by the Go garbage collector.
//
// CopiedBytes is part of the [Borrower] interface.
func (b *ownedBytes) CopiedBytes() []byte {
	if b.owned.ptr == nil {
		return nil
	}

	return C.GoBytes(unsafe.Pointer(b.owned.ptr), C.int(b.owned.len))
}

// intoError converts the ownedBytes into an error. This is used for methods
// that return a ownedBytes as an error type.
//
// If the ownedBytes is nil or has already been freed, it returns nil.
// Otherwise, the bytes will be copied into Go memory and converted into an
// error.
//
// The original ownedBytes will be freed after this operation and is no longer
// valid.
func (b *ownedBytes) intoError() error {
	if b.owned.ptr == nil {
		return nil
	}

	err := errors.New(string(b.CopiedBytes()))

	if err2 := b.Free(); err2 != nil {
		return fmt.Errorf("%w: %w (original error: %w)", errFreeingValue, err, err2)
	}

	return err
}

// newOwnedBytes creates a ownedBytes from a C.OwnedBytes.
//
// The caller is responsible for calling Free() on the returned ownedBytes
// when it is no longer needed otherwise memory will leak.
//
// It is not an error to provide an OwnedBytes with a nil pointer or zero length
// in which case the returned ownedBytes will be empty.
func newOwnedBytes(owned C.OwnedBytes) *ownedBytes {
	return &ownedBytes{owned: owned}
}

// getHashKeyFromHashResult creates a byte slice or error from a C.HashResult.
//
// It returns nil, nil if the result is None.
// It returns nil, err if the result is an error.
// It returns a byte slice, nil if the result is Some.
func getHashKeyFromHashResult(result C.HashResult) (Hash, error) {
	switch result.tag {
	case C.HashResult_NullHandlePointer:
		return EmptyRoot, errDBClosed
	case C.HashResult_None:
		return EmptyRoot, nil
	case C.HashResult_Some:
		cHashKey := (*C.HashKey)(unsafe.Pointer(&result.anon0))
		hashKey := *(*Hash)(unsafe.Pointer(&cHashKey._0))
		return hashKey, nil
	case C.HashResult_Err:
		ownedBytes := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0)))
		return EmptyRoot, ownedBytes.intoError()
	default:
		return EmptyRoot, fmt.Errorf("unknown C.HashResult tag: %d", result.tag)
	}
}

// getErrorgetErrorFromVoidResult converts a C.VoidResult to an error.
//
// It will return nil if the result is Ok, otherwise it returns an error.
func getErrorFromVoidResult(result C.VoidResult) error {
	switch result.tag {
	case C.VoidResult_NullHandlePointer:
		return errDBClosed
	case C.VoidResult_Ok:
		return nil
	case C.VoidResult_Err:
		return newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
	default:
		return fmt.Errorf("unknown C.VoidResult tag: %d", result.tag)
	}
}

// getValueFromValueResult converts a C.ValueResult to a byte slice or error.
//
// It returns nil, nil if the result is None.
// It returns nil, errRevisionNotFound if the result is RevisionNotFound.
// It returns a byte slice, nil if the result is Some.
// It returns an error if the result is an error.
func getValueFromValueResult(result C.ValueResult) ([]byte, error) {
	switch result.tag {
	case C.ValueResult_NullHandlePointer:
		return nil, errDBClosed
	case C.ValueResult_RevisionNotFound:
		// NOTE: the result value contains the provided root hash, we could use
		// it in the error message if needed.
		return nil, errRevisionNotFound
	case C.ValueResult_None:
		return nil, nil
	case C.ValueResult_Some:
		ownedBytes := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0)))
		bytes := ownedBytes.CopiedBytes()
		if err := ownedBytes.Free(); err != nil {
			return nil, fmt.Errorf("%w: %w", errFreeingValue, err)
		}
		return bytes, nil
	case C.ValueResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.ValueResult tag: %d", result.tag)
	}
}

type ownedKeyValueBatch struct {
	owned C.OwnedKeyValueBatch
}

func (b *ownedKeyValueBatch) copy() []*ownedKeyValue {
	if b.owned.ptr == nil {
		return nil
	}
	borrowed := b.borrow()
	copied := make([]*ownedKeyValue, len(borrowed))
	for i, borrow := range borrowed {
		copied[i] = newOwnedKeyValue(borrow)
	}
	return copied
}

func (b *ownedKeyValueBatch) borrow() []C.OwnedKeyValuePair {
	if b.owned.ptr == nil {
		return nil
	}

	return unsafe.Slice((*C.OwnedKeyValuePair)(unsafe.Pointer(b.owned.ptr)), b.owned.len)
}

func (b *ownedKeyValueBatch) free() error {
	if b == nil || b.owned.ptr == nil {
		// we want ownedKeyValueBatch to be typed-nil safe
		return nil
	}

	if err := getErrorFromVoidResult(C.fwd_free_owned_key_value_batch(b.owned)); err != nil {
		return fmt.Errorf("%w: %w", errFreeingValue, err)
	}

	b.owned = C.OwnedKeyValueBatch{}

	return nil
}

// newOwnedKeyValueBatch creates a ownedKeyValueBatch from a C.OwnedKeyValueBatch.
//
// The caller is responsible for calling Free() on the returned ownedKeyValue
// when it is no longer needed otherwise memory will leak.
func newOwnedKeyValueBatch(owned C.OwnedKeyValueBatch) *ownedKeyValueBatch {
	return &ownedKeyValueBatch{
		owned: owned,
	}
}

type ownedKeyValue struct {
	// owned holds the original C-provided pair so we can free it
	// with fwd_free_owned_kv_pair instead of freeing key/value separately.
	owned C.OwnedKeyValuePair
	// key and value wrappers provide Borrowed/Copied accessors
	key   *ownedBytes
	value *ownedBytes
}

func (kv *ownedKeyValue) copy() ([]byte, []byte) {
	key := kv.key.CopiedBytes()
	value := kv.value.CopiedBytes()
	return key, value
}

func (kv *ownedKeyValue) free() error {
	if kv == nil {
		// we want ownedKeyValue to be typed-nil safe
		return nil
	}
	if err := getErrorFromVoidResult(C.fwd_free_owned_kv_pair(kv.owned)); err != nil {
		return fmt.Errorf("%w: %w", errFreeingValue, err)
	}
	// zero out fields to avoid accidental reuse/double free
	kv.owned = C.OwnedKeyValuePair{}
	kv.key = nil
	kv.value = nil
	return nil
}

// newOwnedKeyValue creates a ownedKeyValue from a C.OwnedKeyValuePair.
//
// The caller is responsible for calling Free() on the returned ownedKeyValue
// when it is no longer needed otherwise memory will leak.
func newOwnedKeyValue(owned C.OwnedKeyValuePair) *ownedKeyValue {
	return &ownedKeyValue{
		owned: owned,
		key:   newOwnedBytes(owned.key),
		value: newOwnedBytes(owned.value),
	}
}

// getKeyValueFromResult converts a C.KeyValueResult to a key value pair or error.
//
// It returns nil, nil if the result is None.
// It returns a *ownedKeyValue, nil if the result is Some.
// It returns an error if the result is an error.
func getKeyValueFromResult(result C.KeyValueResult) (*ownedKeyValue, error) {
	switch result.tag {
	case C.KeyValueResult_NullHandlePointer:
		return nil, errDBClosed
	case C.KeyValueResult_None:
		return nil, nil
	case C.KeyValueResult_Some:
		ownedKvp := newOwnedKeyValue(*(*C.OwnedKeyValuePair)(unsafe.Pointer(&result.anon0)))
		return ownedKvp, nil
	case C.KeyValueResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.KeyValueResult tag: %d", result.tag)
	}
}

// getKeyValueBatchFromResult converts a C.KeyValueBatchResult to a key value batch or error.
//
// It returns nil, nil if the result is None.
// It returns a *ownedKeyValueBatch, nil if the result is Some.
// It returns an error if the result is an error.
func getKeyValueBatchFromResult(result C.KeyValueBatchResult) (*ownedKeyValueBatch, error) {
	switch result.tag {
	case C.KeyValueBatchResult_NullHandlePointer:
		return nil, errDBClosed
	case C.KeyValueBatchResult_Some:
		ownedBatch := newOwnedKeyValueBatch(*(*C.OwnedKeyValueBatch)(unsafe.Pointer(&result.anon0)))
		return ownedBatch, nil
	case C.KeyValueBatchResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.KeyValueBatchResult tag: %d", result.tag)
	}
}

// getDatabaseFromHandleResult converts a C.HandleResult to a Database or error.
//
// If the C.HandleResult is an error, it returns an error instead of a Database.
func getDatabaseFromHandleResult(result C.HandleResult) (*Database, error) {
	switch result.tag {
	case C.HandleResult_Ok:
		ptr := *(**C.DatabaseHandle)(unsafe.Pointer(&result.anon0))
		db := &Database{handle: ptr}
		return db, nil
	case C.HandleResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.HandleResult tag: %d", result.tag)
	}
}

func newCHashKey(hash Hash) C.HashKey {
	return *(*C.HashKey)(unsafe.Pointer(&hash))
}

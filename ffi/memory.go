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
	"runtime"
	"unsafe"
)

var (
	errNilStruct     = errors.New("nil struct pointer cannot be freed")
	errBadValue      = errors.New("value from cgo formatted incorrectly")
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
	sliceLen := len(slice)
	if sliceLen == 0 {
		return C.BorrowedBytes{ptr: nil, len: 0}
	}

	ptr := unsafe.SliceData(slice)
	if ptr == nil {
		return C.BorrowedBytes{ptr: nil, len: 0}
	}

	pinner.Pin(ptr)

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

	// TODO: a check for panic will be inserted here
	C.fwd_free_owned_bytes(b.owned)

	// reset the pointer to nil to prevent double freeing
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

// hashAndIDFromValue converts the cgo `Value` payload into:
//
//	case | data    | len   | meaning
//
// 1.    | nil     | 0     | invalid
// 2.    | nil     | non-0 | proposal deleted everything
// 3.    | non-nil | 0     | error string
// 4.    | non-nil | non-0 | hash and id
//
// The value should never be nil.
func hashAndIDFromValue(v *C.struct_Value) ([]byte, uint32, error) {
	// Pin the returned value to prevent it from being garbage collected.
	defer runtime.KeepAlive(v)

	if v == nil {
		return nil, 0, errNilStruct
	}

	if v.data == nil {
		// Case 2
		if v.len != 0 {
			return nil, uint32(v.len), nil
		}

		// Case 1
		return nil, 0, errBadValue
	}

	// Case 3
	if v.len == 0 {
		errStr := C.GoString((*C.char)(unsafe.Pointer(v.data)))
		C.fwd_free_value(v)
		return nil, 0, errors.New(errStr)
	}

	// Case 4
	id := uint32(v.len)
	buf := C.GoBytes(unsafe.Pointer(v.data), RootLength)
	v.len = C.size_t(RootLength) // set the length to free
	C.fwd_free_value(v)
	return buf, id, nil
}

// errorFromValue converts the cgo `Value` payload into:
//
//	case | data    | len   | meaning
//
// 1.    | nil     | 0     | empty
// 2.    | nil     | non-0 | invalid
// 3.    | non-nil | 0     | error string
// 4.    | non-nil | non-0 | invalid
//
// The value should never be nil.
func errorFromValue(v *C.struct_Value) error {
	// Pin the returned value to prevent it from being garbage collected.
	defer runtime.KeepAlive(v)

	if v == nil {
		return errNilStruct
	}

	// Case 1
	if v.data == nil && v.len == 0 {
		return nil
	}

	// Case 3
	if v.len == 0 {
		errStr := C.GoString((*C.char)(unsafe.Pointer(v.data)))
		C.fwd_free_value(v)
		return errors.New(errStr)
	}

	// Case 2 and 4
	C.fwd_free_value(v)
	return errBadValue
}

// bytesFromValue converts the cgo `Value` payload to:
//
//	case | data    | len   | meaning
//
// 1.    | nil     | 0     | empty
// 2.    | nil     | non-0 | invalid
// 3.    | non-nil | 0     | error string
// 4.    | non-nil | non-0 | bytes (most common)
//
// The value should never be nil.
func bytesFromValue(v *C.struct_Value) ([]byte, error) {
	// Pin the returned value to prevent it from being garbage collected.
	defer runtime.KeepAlive(v)

	if v == nil {
		return nil, errNilStruct
	}

	// Case 4
	if v.len != 0 && v.data != nil {
		buf := C.GoBytes(unsafe.Pointer(v.data), C.int(v.len))
		C.fwd_free_value(v)
		return buf, nil
	}

	// Case 1
	if v.len == 0 && v.data == nil {
		return nil, nil
	}

	// Case 3
	if v.len == 0 {
		errStr := C.GoString((*C.char)(unsafe.Pointer(v.data)))
		C.fwd_free_value(v)
		return nil, errors.New(errStr)
	}

	// Case 2
	return nil, errBadValue
}

func databaseFromResult(result *C.struct_DatabaseCreationResult) (*C.DatabaseHandle, error) {
	if result == nil {
		return nil, errNilStruct
	}

	if result.error_str != nil {
		errStr := C.GoString((*C.char)(unsafe.Pointer(result.error_str)))
		C.fwd_free_database_error_result(result)
		runtime.KeepAlive(result)
		return nil, errors.New(errStr)
	}
	return result.db, nil
}

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
)

type Pinner interface {
	Pin(ptr any)
	Unpin()
}

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

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
	"runtime"
	"unsafe"
)

var (
	errNilStruct = errors.New("firewood error: nil struct pointer cannot be freed")
	errBadValue  = errors.New("firewood error: value from cgo formatted incorrectly")
)

// KeyValue is a key-value pair.
type KeyValue struct {
	Key   []byte
	Value []byte
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

// newValueFactory returns a factory for converting byte slices into cgo `Value`
// structs that can be passed as arguments to cgo functions. The returned
// cleanup function MUST be called when the constructed values are no longer
// required, after which they can no longer be used as cgo arguments.
func newValueFactory() (*valueFactory, func()) {
	f := new(valueFactory)
	return f, func() { f.pin.Unpin() }
}

type valueFactory struct {
	pin runtime.Pinner
}

func (f *valueFactory) from(data []byte) C.struct_Value {
	if len(data) == 0 {
		return C.struct_Value{0, nil}
	}
	ptr := (*C.uchar)(unsafe.SliceData(data))
	f.pin.Pin(ptr)
	return C.struct_Value{C.size_t(len(data)), ptr}
}

// createOps creates a slice of cgo `KeyValue` structs from the given keys and
// values and pins the memory of the underlying byte slices to prevent
// garbage collection while the cgo function is using them. The returned cleanup
// function MUST be called when the constructed values are no longer required,
// after which they can no longer be used as cgo arguments.
func createOps(keys, vals [][]byte) ([]C.struct_KeyValue, func()) {
	values, cleanup := newValueFactory()

	ffiOps := make([]C.struct_KeyValue, len(keys))
	for i := range keys {
		ffiOps[i] = C.struct_KeyValue{
			key:   values.from(keys[i]),
			value: values.from(vals[i]),
		}
	}

	return ffiOps, cleanup
}

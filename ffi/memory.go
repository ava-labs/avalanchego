// Package firewood provides a Go wrapper around the [Firewood] database.
//
// [Firewood]: https://github.com/ava-labs/firewood
package firewood

// // Note that -lm is required on Linux but not on Mac.
// #cgo LDFLAGS: -L${SRCDIR}/../target/release -L/usr/local/lib -lfirewood_ffi -lm
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
	errNilBuffer = errors.New("firewood error: nil value returned from cgo")
	errBadValue  = errors.New("firewood error: value from cgo formatted incorrectly")
)

// KeyValue is a key-value pair.
type KeyValue struct {
	Key   []byte
	Value []byte
}

// extractErrorThenFree converts the cgo `Value` payload to a string, frees the
// `Value` if an error is returned, and returns the extracted value.
// If the data is not formatted for a string, and non-null it returns an error.
func extractErrorThenFree(v *C.struct_Value) error {
	if v == nil {
		return errNilBuffer
	}

	// The length isn't expected to be set in either case.
	// May indicate a bug.
	if v.len != 0 {
		// We should still attempt to free the value.
		C.fwd_free_value(v)
		return errBadValue
	}

	// Expected empty case for Rust's `()`
	if v.data == nil {
		return nil
	}

	// If the value is an error string, it should be freed and an error
	// returned.
	go_str := C.GoString((*C.char)(unsafe.Pointer(v.data)))
	C.fwd_free_value(v)
	// Pin the returned value to prevent it from being garbage collected.
	runtime.KeepAlive(v)
	return fmt.Errorf("firewood error: %s", go_str)
}

// extractIdThenFree converts the cgo `Value` payload to a uint32, frees the
// `Value` if an error is returned, and returns the extracted value.
func extractIdThenFree(v *C.struct_Value) (uint32, error) {
	if v == nil {
		return 0, errNilBuffer
	}
	if v.len == 0 && v.data == nil {
		return 0, errBadValue
	}
	// If the value is an error string, it should be freed and an error
	// returned.
	// Any valid ID should be non-zero.
	if v.len == 0 {
		go_str := C.GoString((*C.char)(unsafe.Pointer(v.data)))
		C.fwd_free_value(v)
		// Pin the returned value to prevent it from being garbage collected.
		runtime.KeepAlive(v)
		return 0, fmt.Errorf("firewood error: %s", go_str)
	}
	return uint32(v.len), nil
}

// extractBytesThenFree converts the cgo `Value` payload to a byte slice, frees
// the `Value`, and returns the extracted slice.
// Generates error if the error term is nonnull.
func extractBytesThenFree(v *C.struct_Value) (buf []byte, err error) {
	if v == nil || v.data == nil {
		return nil, nil
	}

	buf = C.GoBytes(unsafe.Pointer(v.data), C.int(v.len))
	if v.len == 0 {
		errStr := C.GoString((*C.char)(unsafe.Pointer(v.data)))
		err = fmt.Errorf("firewood error: %s", errStr)
	}
	C.fwd_free_value(v)

	// Pin the returned value to prevent it from being garbage collected.
	runtime.KeepAlive(v)
	return
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

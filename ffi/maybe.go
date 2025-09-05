// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"unsafe"
)

// Maybe is an interface that represents an optional value of type T.
//
// Maybe is a drop-in replacement for the Maybe type defined in avalanchego/utils/maybe.
// This interface is used to avoid importing avalanchego packages into the ffi
// package, which would create a circular dependency.
//
// <https://github.com/ava-labs/avalanchego/blob/66ca7dc0ce354ff4c4ebdc8b377e099ba91f2e2b/utils/maybe/maybe.go#L40-L48>
//
// The avalanchego implementation of Maybe implements this interface.
type Maybe[T any] interface {
	// HasValue returns true if the Maybe contains a value.
	HasValue() bool
	// Value returns the value contained in the Maybe.
	//
	// Implementations may panic if the Maybe contains no value but can also
	// return the zero value of T.
	Value() T
}

func newMaybeBorrowedBytes(maybe Maybe[[]byte], pinner Pinner) C.Maybe_BorrowedBytes {
	var cMaybe C.Maybe_BorrowedBytes

	if maybe != nil && maybe.HasValue() {
		cMaybeBorrowedBytesPtr := (*C.BorrowedBytes)(unsafe.Pointer(&cMaybe.anon0))
		*cMaybeBorrowedBytesPtr = newBorrowedBytes(maybe.Value(), pinner)

		cMaybe.tag = C.Maybe_BorrowedBytes_Some_BorrowedBytes
	} else {
		cMaybe.tag = C.Maybe_BorrowedBytes_None_BorrowedBytes
	}

	return cMaybe
}

func (b *ownedBytes) HasValue() bool {
	return b != nil
}

func (b *ownedBytes) Value() *ownedBytes {
	return b
}

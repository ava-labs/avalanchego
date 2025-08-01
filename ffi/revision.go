// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// Package ffi provides a Go wrapper around the [Firewood] database.
//
// [Firewood]: https://github.com/ava-labs/firewood
package ffi

// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"errors"
	"fmt"
)

var (
	errRevisionNotFound  = errors.New("revision not found")
	errInvalidRootLength = fmt.Errorf("root hash must be %d bytes", RootLength)
)

type Revision struct {
	// handle is returned and accepted by cgo functions. It MUST be treated as
	// an opaque value without special meaning.
	// https://en.wikipedia.org/wiki/Blinkenlights
	handle *C.DatabaseHandle
	// The revision root
	root []byte
}

func newRevision(handle *C.DatabaseHandle, root []byte) (*Revision, error) {
	if handle == nil {
		return nil, errDBClosed
	}

	// Check that the root is the correct length.
	if root == nil || len(root) != RootLength {
		return nil, errInvalidRootLength
	}

	// Attempt to get any value from the root.
	// This will verify that the root is valid and accessible.
	// If the root is not valid, this will return an error.
	values, cleanup := newValueFactory()
	defer cleanup()
	val := C.fwd_get_from_root(handle, values.from(root), values.from([]byte{}))
	_, err := bytesFromValue(&val)
	if err != nil {
		// Any error from this function indicates that the root is inaccessible.
		return nil, errRevisionNotFound
	}

	// All other verification of the root is done during use.
	return &Revision{
		handle: handle,
		root:   root,
	}, nil
}

func (r *Revision) Get(key []byte) ([]byte, error) {
	if r.handle == nil {
		return nil, errDBClosed
	}
	if r.root == nil {
		return nil, errRevisionNotFound
	}

	values, cleanup := newValueFactory()
	defer cleanup()

	val := C.fwd_get_from_root(r.handle, values.from(r.root), values.from(key))
	value, err := bytesFromValue(&val)
	if err != nil {
		// Any error from this function indicates that the revision is inaccessible.
		r.root = nil
	}
	return value, err
}

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
)

var (
	errRevisionClosed = errors.New("firewood revision already closed")
	errInvalidRoot    = fmt.Errorf("firewood error: root hash must be %d bytes", RootLength)
)

type Revision struct {
	// handle is returned and accepted by cgo functions. It MUST be treated as
	// an opaque value without special meaning.
	// https://en.wikipedia.org/wiki/Blinkenlights
	handle *C.DatabaseHandle
	// The revision root
	root []byte
}

func NewRevision(handle *C.DatabaseHandle, root []byte) (*Revision, error) {
	if handle == nil {
		return nil, errors.New("firewood error: nil handle or root")
	}

	// Check that the root is the correct length.
	if root == nil || len(root) != RootLength {
		return nil, errInvalidRoot
	}

	// All other verification of the root is done during use.
	return &Revision{
		handle: handle,
		root:   root,
	}, nil
}

func (r *Revision) Get(key []byte) ([]byte, error) {
	if r.handle == nil {
		return nil, errDbClosed
	}
	if r.root == nil {
		return nil, errRevisionClosed
	}

	values, cleanup := newValueFactory()
	defer cleanup()

	val := C.fwd_get_from_root(r.handle, values.from(r.root), values.from(key))
	value, err := extractBytesThenFree(&val)
	if err != nil {
		// Any error from this function indicates that the revision is inaccessible.
		r.root = nil
	}
	return value, err
}

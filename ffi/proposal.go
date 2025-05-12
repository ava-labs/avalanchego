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
)

var errDroppedProposal = errors.New("proposal already dropped")

type Proposal struct {
	// handle is returned and accepted by cgo functions. It MUST be treated as
	// an opaque value without special meaning.
	// https://en.wikipedia.org/wiki/Blinkenlights
	handle *C.DatabaseHandle

	// The proposal ID.
	// id = 0 is reserved for a dropped proposal.
	id uint32
}

func (p *Proposal) Get(key []byte) ([]byte, error) {
	if p.handle == nil {
		return nil, errDbClosed
	}

	if p.id == 0 {
		return nil, errDroppedProposal
	}
	values, cleanup := newValueFactory()
	defer cleanup()

	// Get the value for the given key.
	val := C.fwd_get_from_proposal(p.handle, C.uint32_t(p.id), values.from(key))
	return extractBytesThenFree(&val)
}

func (p *Proposal) Commit() error {
	if p.handle == nil {
		return errDbClosed
	}

	if p.id == 0 {
		return errDroppedProposal
	}

	// Commit the proposal and return the hash.
	err_val := C.fwd_commit(p.handle, C.uint32_t(p.id))
	err := extractErrorThenFree(&err_val)
	if err != nil {
		// this is unrecoverable due to Rust's ownership model
		// The underlying proposal is no longer valid.
		p.id = 0
	}
	return err
}

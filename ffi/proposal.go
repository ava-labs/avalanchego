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
	"unsafe"
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

// Get retrieves the value for the given key.
// If the key does not exist, it returns (nil, nil).
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

// Propose creates a new proposal with the given keys and values.
// The proposal is not committed until Commit is called.
func (p *Proposal) Propose(keys, vals [][]byte) (*Proposal, error) {
	if p.handle == nil {
		return nil, errDbClosed
	}

	if p.id == 0 {
		return nil, errDroppedProposal
	}

	ops := make([]KeyValue, len(keys))
	for i := range keys {
		ops[i] = KeyValue{keys[i], vals[i]}
	}

	values, cleanup := newValueFactory()
	defer cleanup()

	ffiOps := make([]C.struct_KeyValue, len(ops))
	for i, op := range ops {
		ffiOps[i] = C.struct_KeyValue{
			key:   values.from(op.Key),
			value: values.from(op.Value),
		}
	}

	// Propose the keys and values.
	val := C.fwd_propose_on_proposal(p.handle, C.uint32_t(p.id),
		C.size_t(len(ffiOps)),
		(*C.struct_KeyValue)(unsafe.SliceData(ffiOps)),
	)
	id, err := extractIdThenFree(&val)
	if err != nil {
		return nil, err
	}

	return &Proposal{
		handle: p.handle,
		id:     id,
	}, nil
}

// Commit commits the proposal and returns any errors.
// If an error occurs, the proposal is dropped and no longer valid.
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

// Drop removes the proposal from memory in Firewood.
// In the case of an error, the proposal can assumed to be dropped.
// An error is returned if the proposal was already dropped.
func (p *Proposal) Drop() error {
	if p.handle == nil {
		return errDbClosed
	}

	if p.id == 0 {
		return errDroppedProposal
	}

	// Drop the proposal.
	val := C.fwd_drop_proposal(p.handle, C.uint32_t(p.id))
	p.id = 0
	return extractErrorThenFree(&val)
}

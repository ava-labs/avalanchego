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
	"runtime"
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

	// The proposal root hash.
	root []byte
}

// newProposal creates a new Proposal from the given DatabaseHandle and Value.
// The Value must be returned from a Firewood FFI function.
// An error can only occur from parsing the Value.
func newProposal(handle *C.DatabaseHandle, val *C.struct_Value) (*Proposal, error) {
	bytes, id, err := hashAndIDFromValue(val)
	if err != nil {
		return nil, err
	}

	// If the proposal root is nil, it means the proposal is empty.
	if bytes == nil {
		bytes = make([]byte, RootLength)
	}

	return &Proposal{
		handle: handle,
		id:     id,
		root:   bytes,
	}, nil
}

// Root retrieves the root hash of the proposal.
// If the proposal is empty (i.e. no keys in database),
// it returns nil, nil.
func (p *Proposal) Root() ([]byte, error) {
	if p.handle == nil {
		return nil, errDBClosed
	}

	if p.id == 0 {
		return nil, errDroppedProposal
	}

	// If the hash is empty, return the empty root hash.
	if p.root == nil {
		return make([]byte, RootLength), nil
	}

	// Get the root hash of the proposal.
	return p.root, nil
}

// Get retrieves the value for the given key.
// If the key does not exist, it returns (nil, nil).
func (p *Proposal) Get(key []byte) ([]byte, error) {
	if p.handle == nil {
		return nil, errDBClosed
	}

	if p.id == 0 {
		return nil, errDroppedProposal
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	// Get the value for the given key.
	return getValueFromValueResult(C.fwd_get_from_proposal(p.handle, C.uint32_t(p.id), newBorrowedBytes(key, &pinner)))
}

// Propose creates a new proposal with the given keys and values.
// The proposal is not committed until Commit is called.
func (p *Proposal) Propose(keys, vals [][]byte) (*Proposal, error) {
	if p.handle == nil {
		return nil, errDBClosed
	}

	if p.id == 0 {
		return nil, errDroppedProposal
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	kvp, err := newKeyValuePairs(keys, vals, &pinner)
	if err != nil {
		return nil, err
	}

	// Propose the keys and values.
	val := C.fwd_propose_on_proposal(p.handle, C.uint32_t(p.id), kvp)

	return newProposal(p.handle, &val)
}

// Commit commits the proposal and returns any errors.
// If an error occurs, the proposal is dropped and no longer valid.
func (p *Proposal) Commit() error {
	if p.handle == nil {
		return errDBClosed
	}

	if p.id == 0 {
		return errDroppedProposal
	}

	_, err := getHashKeyFromHashResult(C.fwd_commit(p.handle, C.uint32_t(p.id)))
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
		return errDBClosed
	}

	if p.id == 0 {
		return errDroppedProposal
	}

	// Drop the proposal.
	val := C.fwd_drop_proposal(p.handle, C.uint32_t(p.id))
	p.id = 0
	return errorFromValue(&val)
}

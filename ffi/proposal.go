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
	"runtime"
	"sync"
	"unsafe"
)

var errDroppedProposal = errors.New("proposal already dropped")

type Proposal struct {
	// handle is an opaque pointer to the proposal within Firewood. It should be
	// passed to the C FFI functions that operate on proposals
	//
	// It is not safe to call these methods with a nil handle.
	//
	// Calls to `C.fwd_commit_proposal` and `C.fwd_free_proposal` will invalidate
	// this handle, so it should not be used after those calls.
	handle *C.ProposalHandle
	disown sync.Mutex
	// [Database.Close] blocks on this WaitGroup, which is incremented by
	// [getProposalFromProposalResult], and decremented by either
	// [Proposal.Commit] or [Proposal.Drop] (when the handle is disowned).
	openProposals *sync.WaitGroup

	// The proposal root hash.
	root []byte
}

// Root retrieves the root hash of the proposal.
// If the proposal is empty (i.e. no keys in database),
// it returns nil, nil.
func (p *Proposal) Root() ([]byte, error) {
	return p.root, nil
}

// Get retrieves the value for the given key.
// If the key does not exist, it returns (nil, nil).
func (p *Proposal) Get(key []byte) ([]byte, error) {
	if p.handle == nil {
		return nil, errDroppedProposal
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	return getValueFromValueResult(C.fwd_get_from_proposal(p.handle, newBorrowedBytes(key, &pinner)))
}

// Iter creates and iterator starting from the provided key on proposal.
// pass empty slice to start from beginning
func (p *Proposal) Iter(key []byte) (*Iterator, error) {
	if p.handle == nil {
		return nil, errDBClosed
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	itResult := C.fwd_iter_on_proposal(p.handle, newBorrowedBytes(key, &pinner))

	return getIteratorFromIteratorResult(itResult)
}

// Propose is equivalent to [Database.Propose] except that the new proposal is
// based on `p`.
//
// Value Semantics:
//   - nil value (vals[i] == nil): Performs a DeleteRange operation using the key as a prefix
//   - empty slice (vals[i] != nil && len(vals[i]) == 0): Inserts/updates the key with an empty value
//   - non-empty value: Inserts/updates the key with the provided value
func (p *Proposal) Propose(keys, vals [][]byte) (*Proposal, error) {
	if p.handle == nil {
		return nil, errDroppedProposal
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	kvp, err := newKeyValuePairs(keys, vals, &pinner)
	if err != nil {
		return nil, err
	}
	return getProposalFromProposalResult(C.fwd_propose_on_proposal(p.handle, kvp), p.openProposals)
}

// disownHandle is the common path of [Proposal.Commit] and [Proposal.Drop], the
// `fn` argument defining the method-specific behaviour.
func (p *Proposal) disownHandle(fn func(*C.ProposalHandle) error, disownEvenOnErr bool) error {
	p.disown.Lock()
	defer p.disown.Unlock()

	if p.handle == nil {
		return errDroppedProposal
	}
	err := fn(p.handle)
	if disownEvenOnErr || err == nil {
		p.handle = nil
		p.openProposals.Done()
	}
	return err
}

// Commit commits the proposal and returns any errors.
//
// The proposal handle is no longer valid after this call, but the root
// hash can still be retrieved using Root().
func (p *Proposal) Commit() error {
	return p.disownHandle(commitProposal, true)
}

func commitProposal(h *C.ProposalHandle) error {
	_, err := getHashKeyFromHashResult(C.fwd_commit_proposal(h))
	return err
}

// Drop releases the memory associated with the Proposal.
//
// This is safe to call if the pointer is nil, in which case it does nothing.
//
// The pointer will be set to nil after freeing to prevent double free.
func (p *Proposal) Drop() error {
	if err := p.disownHandle(dropProposal, false); err != nil && err != errDroppedProposal {
		return err
	}
	return nil
}

func dropProposal(h *C.ProposalHandle) error {
	if err := getErrorFromVoidResult(C.fwd_free_proposal(h)); err != nil {
		return fmt.Errorf("%w: %w", errFreeingValue, err)
	}
	return nil
}

// getProposalFromProposalResult converts a C.ProposalResult to a Proposal or error.
func getProposalFromProposalResult(result C.ProposalResult, openProposals *sync.WaitGroup) (*Proposal, error) {
	switch result.tag {
	case C.ProposalResult_NullHandlePointer:
		return nil, errDBClosed
	case C.ProposalResult_Ok:
		body := (*C.ProposalResult_Ok_Body)(unsafe.Pointer(&result.anon0))
		hashKey := *(*[32]byte)(unsafe.Pointer(&body.root_hash._0))
		proposal := &Proposal{
			handle:        body.handle,
			root:          hashKey[:],
			openProposals: openProposals,
		}
		openProposals.Add(1)
		runtime.SetFinalizer(proposal, (*Proposal).Drop)
		return proposal, nil
	case C.ProposalResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.ProposalResult tag: %d", result.tag)
	}
}

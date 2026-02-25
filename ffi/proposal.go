// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

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

// Proposal represents a set of proposed changes to be committed to the database.
// Proposals are created via [Database.Propose] or [Proposal.Propose], and must be
// either committed with [Proposal.Commit] or released with [Proposal.Drop].
//
// Proposals must be committed or dropped before the associated database is
// closed. A finalizer is set on each Proposal to ensure that Drop is called
// when the Proposal is garbage collected, but relying on finalizers is not
// recommended. Failing to commit or drop a proposal before the database is
// closed will cause it to block or fail.
//
// All read operations on a Proposal are thread-safe with respect to each other,
// and can be performed regardless of the state of the associated database. However,
// proposals cannot be committed concurrently with other operations that access
// the latest state of the database, such as [Database.Get] and [Database.Propose],
type Proposal struct {
	// handle is an opaque pointer to the proposal within Firewood. It should be
	// passed to the C FFI functions that operate on proposals
	//
	// It is not safe to call these methods with a nil handle.
	//
	// Calls to `C.fwd_commit_proposal` and `C.fwd_free_proposal` will invalidate
	// this handle, so it should not be used after those calls.
	handle *C.ProposalHandle

	// root is the root hash of the proposal and the expected root hash after commit.
	root Hash

	// keepAliveHandle is used to keep the database alive while this proposal is
	// in use. It is initialized when the proposal is created and disowned after
	// [Proposal.Commit] or [Proposal.Drop] is called.
	keepAliveHandle databaseKeepAliveHandle

	// commitLock is used to ensure that methods accessing the latest state do not conflict.
	commitLock *sync.Mutex
}

// Root retrieves the root hash of the proposal.
func (p *Proposal) Root() Hash {
	return p.root
}

// Get retrieves the value for the given key.
// If the key does not exist, it returns nil.
func (p *Proposal) Get(key []byte) ([]byte, error) {
	if p.handle == nil {
		return nil, errDroppedProposal
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	return getValueFromValueResult(C.fwd_get_from_proposal(p.handle, newBorrowedBytes(key, &pinner)))
}

// Iter creates an [Iterator] over the key-value pairs in this proposal,
// starting at the first key greater than or equal to the provided key.
// Pass nil or an empty slice to iterate from the beginning.
//
// The Iterator must be released with [Iterator.Drop] when no longer needed,
// otherwise the underlying proposal will never be properly freed.
//
// It returns an error if Drop or Commit has already been called on the Proposal.
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
// The returned proposal cannot be committed until the parent proposal `p` has been
// committed. Additionally, it must be committed or dropped before the [Database] is closed.
//
// Use [Put], [Delete], and [PrefixDelete] to create batch operations.
func (p *Proposal) Propose(batch []BatchOp) (*Proposal, error) {
	if p.handle == nil {
		return nil, errDroppedProposal
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	kvp := newKeyValuePairsFromBatch(batch, &pinner)
	return getProposalFromProposalResult(C.fwd_propose_on_proposal(p.handle, kvp), p.keepAliveHandle.outstandingHandles, p.commitLock)
}

// Commit commits the proposal and returns any errors.
//
// The underlying data is no longer available during/after this call, but the root
// hash can still be retrieved using [Proposal.Root].
// This is safe to call only once; subsequent calls will return an error.
//
// During this function, the latest state of the database is locked, so other
// operations that access it (such as [Database.Get] and [Database.Propose]) will
// block until this function returns.
func (p *Proposal) Commit() error {
	return p.keepAliveHandle.disown(true /* evenOnError */, func() error {
		if p.handle == nil {
			return errDroppedProposal
		}

		p.commitLock.Lock()
		resp := C.fwd_commit_proposal(p.handle)
		p.commitLock.Unlock()
		_, err := getHashKeyFromHashResult(resp)

		// Prevent double free
		p.handle = nil

		return err
	})
}

// Drop releases the memory associated with the Proposal. All child proposals
// created from this proposal can no longer be committed.
//
// This is safe to call if the memory has already been released, in which case
// it does nothing.
func (p *Proposal) Drop() error {
	return p.keepAliveHandle.disown(false /* evenOnError */, func() error {
		if p.handle == nil {
			return nil
		}

		if err := getErrorFromVoidResult(C.fwd_free_proposal(p.handle)); err != nil {
			return fmt.Errorf("%w: %w", errFreeingValue, err)
		}

		// Prevent double free
		p.handle = nil

		return nil
	})
}

// Dump returns a DOT (Graphviz) format representation of the trie structure
// of this proposal for debugging purposes.
//
// Returns errDroppedProposal if Commit or Drop has already been called.
func (p *Proposal) Dump() (string, error) {
	if p.handle == nil {
		return "", errDroppedProposal
	}

	bytes, err := getValueFromValueResult(C.fwd_proposal_dump(p.handle))
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

// getProposalFromProposalResult converts a C.ProposalResult to a Proposal or error.
func getProposalFromProposalResult(result C.ProposalResult, wg *sync.WaitGroup, commitLock *sync.Mutex) (*Proposal, error) {
	switch result.tag {
	case C.ProposalResult_NullHandlePointer:
		return nil, errDBClosed
	case C.ProposalResult_Ok:
		body := (*C.ProposalResult_Ok_Body)(unsafe.Pointer(&result.anon0))
		hashKey := *(*Hash)(unsafe.Pointer(&body.root_hash._0))
		proposal := &Proposal{
			handle:     body.handle,
			root:       hashKey,
			commitLock: commitLock,
		}
		proposal.keepAliveHandle.init(wg)
		runtime.SetFinalizer(proposal, (*Proposal).Drop)
		return proposal, nil
	case C.ProposalResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.ProposalResult tag: %d", result.tag)
	}
}

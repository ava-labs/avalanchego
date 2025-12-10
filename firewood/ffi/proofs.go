// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"encoding"
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

var (
	errNotPrepared = errors.New("proof not prepared into a proposal or committed")
	errEmptyTrie   = errors.New("a range proof was requested on an empty trie")
)

var (
	_ encoding.BinaryMarshaler   = (*RangeProof)(nil)
	_ encoding.BinaryUnmarshaler = (*RangeProof)(nil)
)

// RangeProof represents a proof that a range of keys and their values are
// included in a trie with a given root hash.
//
// RangeProofs can be created via [Database.RangeProof] and are marshallable via
// [encoding.BinaryMarshaler] and [encoding.BinaryUnmarshaler]. They can be
// verified independent of a database via [RangeProof.Verify] or with a database
// via [Database.VerifyRangeProof]. Verified range proofs can be committed to a
// database via [Database.VerifyAndCommitRangeProof] (where verification will
// be skipped if it was already verified). Verifying a range proof with a
// database will optimistically prepare a proposal that can be committed later.
type RangeProof struct {
	// handle is an opaque pointer to the range proof within Firewood. It should be
	// passed to the C FFI functions that operate on range proofs
	//
	// It is not safe to call these methods with a nil handle.
	//
	// Calls to `C.fwd_free_range_proof` will invalidate this handle, so it
	// should not be used after those calls.
	//
	// Calls to `C.fwd_db_verify_range_proof` will cause the range proof to
	// build and retain ownership of an embedded proposal, which also retains
	// a reference to the database. Therefore, while the range proof owns an
	// embedded proposal, the database must be kept alive. The proposal and
	// reference to the database are released after calling
	// `C.fwd_db_verify_and_commit_range_proof` or `C.fwd_free_range_proof`.
	handle *C.RangeProofContext

	// keepAliveHandle keeps the database alive while this range proof
	// owns an embedded proposal. It is initialized when the range proof is
	// verified with a database handle ([Database.VerifyRangeProof]) and not by
	// unmarshalling or when [RangeProof.Verify] is used. It is disowned after
	// [Database.VerifyAndCommitRangeProof] or [RangeProof.Free].
	keepAliveHandle databaseKeepAliveHandle
}

// ChangeProof represents a proof of changes between two roots for a range of keys.
type ChangeProof struct {
	handle *C.ChangeProofContext
}

// NextKeyRange represents a range of keys to fetch from the database. The start
// key is inclusive while the end key is exclusive. If the end key is Nothing,
// the range is unbounded in that direction.
type NextKeyRange struct {
	startKey *ownedBytes
	endKey   Maybe[*ownedBytes]
}

// RangeProof returns a proof that the values in the range [startKey, endKey] are
// included in the tree with the current root. The proof may be truncated to at
// most [maxLength] entries, if non-zero. If either [startKey] or [endKey] is
// Nothing, the range is unbounded in that direction. If [rootHash] is Nothing, the
// current root of the database is used.
func (db *Database) RangeProof(
	rootHash Hash,
	startKey, endKey Maybe[[]byte],
	maxLength uint32,
) (*RangeProof, error) {
	if db.handle == nil {
		return nil, errDBClosed
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	args := C.CreateRangeProofArgs{
		root:       newCHashKey(rootHash),
		start_key:  newMaybeBorrowedBytes(startKey, &pinner),
		end_key:    newMaybeBorrowedBytes(endKey, &pinner),
		max_length: C.uint32_t(maxLength),
	}

	return getRangeProofFromRangeProofResult(C.fwd_db_range_proof(db.handle, args))
}

// Verify verifies the provided range [proof] proves the values in the range
// [startKey, endKey] are included in the tree with the given [rootHash]. If the
// proof is valid, nil is returned; otherwise an error describing why the proof is
// invalid is returned.
func (p *RangeProof) Verify(
	rootHash Hash,
	startKey, endKey Maybe[[]byte],
	maxLength uint32,
) error {
	var pinner runtime.Pinner
	defer pinner.Unpin()

	args := C.VerifyRangeProofArgs{
		proof:      p.handle,
		root:       newCHashKey(rootHash),
		start_key:  newMaybeBorrowedBytes(startKey, &pinner),
		end_key:    newMaybeBorrowedBytes(endKey, &pinner),
		max_length: C.uint32_t(maxLength),
	}

	return getErrorFromVoidResult(C.fwd_range_proof_verify(args))
}

// VerifyChangeProof verifies the provided change [proof] proves the changes
// between [startRoot] and [endRoot] for keys in the range [startKey, endKey]. If
// the proof is valid, a proposal containing the changes is prepared. The
// call to [*Database.VerifyAndCommitRangeProof] will skip verification and commit the
// prepared proposal.
//
// Because this method prepares a proposal, the database must be kept alive
// until the proof is committed or freed.
func (db *Database) VerifyRangeProof(
	proof *RangeProof,
	startKey, endKey Maybe[[]byte],
	rootHash Hash,
	maxLength uint32,
) error {
	var pinner runtime.Pinner
	defer pinner.Unpin()

	args := C.VerifyRangeProofArgs{
		proof:      proof.handle,
		root:       newCHashKey(rootHash),
		start_key:  newMaybeBorrowedBytes(startKey, &pinner),
		end_key:    newMaybeBorrowedBytes(endKey, &pinner),
		max_length: C.uint32_t(maxLength),
	}

	if err := getErrorFromVoidResult(C.fwd_db_verify_range_proof(db.handle, args)); err != nil {
		return err
	}

	// keep the database alive while the proof owns the embedded proposal
	proof.keepAliveHandle.init(&db.outstandingHandles)
	runtime.SetFinalizer(proof, (*RangeProof).Free)
	return nil
}

// VerifyAndCommitRangeProof verifies the provided range [proof] proves the values
// in the range [startKey, endKey] are included in the tree with the given
// [rootHash]. If the proof is valid, it is committed to the database and the
// new root hash is returned. The resulting root hash may not equal the
// provided root hash if the proof was truncated due to [maxLength].
func (db *Database) VerifyAndCommitRangeProof(
	proof *RangeProof,
	startKey, endKey Maybe[[]byte],
	rootHash Hash,
	maxLength uint32,
) (Hash, error) {
	if db.handle == nil {
		return EmptyRoot, errDBClosed
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	args := C.VerifyRangeProofArgs{
		proof:      proof.handle,
		root:       newCHashKey(rootHash),
		start_key:  newMaybeBorrowedBytes(startKey, &pinner),
		end_key:    newMaybeBorrowedBytes(endKey, &pinner),
		max_length: C.uint32_t(maxLength),
	}

	var hash Hash
	err := proof.keepAliveHandle.disown(true /* evenOnError */, func() error {
		var err error
		hash, err = getHashKeyFromHashResult(C.fwd_db_verify_and_commit_range_proof(db.handle, args))
		return err
	})
	return hash, err
}

// FindNextKey returns the next key range to fetch for this proof, if any. If the
// proof has been fully processed, nil is returned. If an error occurs while
// determining the next key range, that error is returned.
//
// FindNextKey can only be called after a successful call to [*Database.VerifyRangeProof] or
// [*Database.VerifyAndCommitRangeProof].
func (p *RangeProof) FindNextKey() (*NextKeyRange, error) {
	return getNextKeyRangeFromNextKeyRangeResult(C.fwd_range_proof_find_next_key(p.handle))
}

// MarshalBinary returns a serialized representation of this RangeProof.
//
// The format is unspecified and opaque to firewood.
func (p *RangeProof) MarshalBinary() ([]byte, error) {
	return getValueFromValueResult(C.fwd_range_proof_to_bytes(p.handle))
}

// UnmarshalBinary sets the contents of this RangeProof to be the deserialized
// form of [data] overwriting any existing contents.
func (p *RangeProof) UnmarshalBinary(data []byte) error {
	if err := p.Free(); err != nil {
		return err
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	handle, err := getRangeProofFromRangeProofResult(
		C.fwd_range_proof_from_bytes(newBorrowedBytes(data, &pinner)))

	if err == nil {
		p.handle = handle.handle
		handle.handle = nil
	}

	return err
}

// Free releases the resources associated with this RangeProof.
//
// It is safe to call Free more than once; subsequent calls after the first
// will be no-ops.
func (p *RangeProof) Free() error {
	return p.keepAliveHandle.disown(false /* evenOnError */, func() error {
		if p.handle == nil {
			return nil
		}

		if err := getErrorFromVoidResult(C.fwd_free_range_proof(p.handle)); err != nil {
			return err
		}

		p.handle = nil
		return nil
	})
}

// ChangeProof returns a proof that the changes between [startRoot] and
// [endRoot] for keys in the range [startKey, endKey]. The proof may be
// truncated to at most [maxLength] entries, if non-zero. If either [startKey] or
// [endKey] is Nothing, the range is unbounded in that direction.
func (db *Database) ChangeProof(
	startRoot, endRoot Hash,
	startKey, endKey Maybe[[]byte],
	maxLength uint32,
) (*ChangeProof, error) {
	if db.handle == nil {
		return nil, errDBClosed
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	args := C.CreateChangeProofArgs{
		start_root: newCHashKey(startRoot),
		end_root:   newCHashKey(endRoot),
		start_key:  newMaybeBorrowedBytes(startKey, &pinner),
		end_key:    newMaybeBorrowedBytes(endKey, &pinner),
		max_length: C.uint32_t(maxLength),
	}

	return getChangeProofFromChangeProofResult(C.fwd_db_change_proof(db.handle, args))
}

// VerifyChangeProof verifies the provided change [proof] proves the changes
// between [startRoot] and [endRoot] for keys in the range [startKey, endKey]. If
// the proof is valid, a proposal containing the changes is prepared. The call
// to [*Database.VerifyAndCommitChangeProof] will skip verification and commit the
// prepared proposal.
func (db *Database) VerifyChangeProof(
	proof *ChangeProof,
	startRoot, endRoot Hash,
	startKey, endKey Maybe[[]byte],
	maxLength uint32,
) error {
	var pinner runtime.Pinner
	defer pinner.Unpin()

	args := C.VerifyChangeProofArgs{
		proof:      proof.handle,
		start_root: newCHashKey(startRoot),
		end_root:   newCHashKey(endRoot),
		start_key:  newMaybeBorrowedBytes(startKey, &pinner),
		end_key:    newMaybeBorrowedBytes(endKey, &pinner),
		max_length: C.uint32_t(maxLength),
	}

	return getErrorFromVoidResult(C.fwd_db_verify_change_proof(db.handle, args))
}

// VerifyAndCommitChangeProof verifies the provided change [proof] proves the changes
// between [startRoot] and [endRoot] for keys in the range [startKey, endKey]. If
// the proof is valid, it is committed to the database and the new root hash is
// returned. The resulting root hash may not equal the end root if the proof was
// truncated due to [maxLength].
func (db *Database) VerifyAndCommitChangeProof(
	proof *ChangeProof,
	startRoot, endRoot Hash,
	startKey, endKey Maybe[[]byte],
	maxLength uint32,
) (Hash, error) {
	if db.handle == nil {
		return EmptyRoot, errDBClosed
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	args := C.VerifyChangeProofArgs{
		proof:      proof.handle,
		start_root: newCHashKey(startRoot),
		end_root:   newCHashKey(endRoot),
		start_key:  newMaybeBorrowedBytes(startKey, &pinner),
		end_key:    newMaybeBorrowedBytes(endKey, &pinner),
		max_length: C.uint32_t(maxLength),
	}

	return getHashKeyFromHashResult(C.fwd_db_verify_and_commit_change_proof(db.handle, args))
}

// FindNextKey returns the next key range to fetch for this proof, if any. If the
// proof has been fully processed, nil is returned. If an error occurs while
// determining the next key range, that error is returned.
//
// FindNextKey can only be called after a successful call to [*Database.VerifyChangeProof] or
// [*Database.VerifyAndCommitChangeProof].
func (p *ChangeProof) FindNextKey() (*NextKeyRange, error) {
	return getNextKeyRangeFromNextKeyRangeResult(C.fwd_change_proof_find_next_key(p.handle))
}

// MarshalBinary returns a serialized representation of this ChangeProof.
//
// The format is unspecified and opaque to firewood.
func (p *ChangeProof) MarshalBinary() ([]byte, error) {
	return getValueFromValueResult(C.fwd_change_proof_to_bytes(p.handle))
}

// UnmarshalBinary sets the contents of this ChangeProof to be the deserialized
// form of [data] overwriting any existing contents.
func (p *ChangeProof) UnmarshalBinary(data []byte) error {
	if err := p.Free(); err != nil {
		return err
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	handle, err := getChangeProofFromChangeProofResult(
		C.fwd_change_proof_from_bytes(newBorrowedBytes(data, &pinner)))

	if err == nil {
		p.handle = handle.handle
		handle.handle = nil
	}

	return err
}

// Free releases the resources associated with this ChangeProof.
//
// It is safe to call Free more than once; subsequent calls after the first
// will be no-ops.
func (p *ChangeProof) Free() error {
	if p.handle == nil {
		return nil
	}

	if err := getErrorFromVoidResult(C.fwd_free_change_proof(p.handle)); err != nil {
		return err
	}

	p.handle = nil

	return nil
}

// StartKey returns the inclusive start key of this key range.
func (r *NextKeyRange) StartKey() []byte {
	return r.startKey.BorrowedBytes()
}

// HasEndKey returns true if this key range has an exclusive end key.
func (r *NextKeyRange) HasEndKey() bool {
	return r.endKey.HasValue()
}

// EndKey returns the exclusive end key of this key range if it exists or nil if
// it does not.
func (r *NextKeyRange) EndKey() []byte {
	if r.endKey.HasValue() {
		return r.endKey.Value().BorrowedBytes()
	}
	return nil
}

// Free releases the resources associated with this NextKeyRange.
//
// It is safe to call Free more than once; subsequent calls after the first
// will be no-ops.
func (r *NextKeyRange) Free() error {
	var err1, err2 error

	err1 = r.startKey.Free()
	if r.endKey != nil && r.endKey.HasValue() {
		err2 = r.endKey.Value().Free()
	}

	return errors.Join(err1, err2)
}

func newNextKeyRange(cRange C.NextKeyRange) *NextKeyRange {
	var nextKeyRange NextKeyRange

	nextKeyRange.startKey = newOwnedBytes(cRange.start_key)

	if cRange.end_key.tag == C.Maybe_OwnedBytes_Some_OwnedBytes {
		nextKeyRange.endKey = newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&cRange.end_key.anon0)))
	}

	return &nextKeyRange
}

func getNextKeyRangeFromNextKeyRangeResult(result C.NextKeyRangeResult) (*NextKeyRange, error) {
	switch result.tag {
	case C.NextKeyRangeResult_NullHandlePointer:
		return nil, errDBClosed
	case C.NextKeyRangeResult_NotPrepared:
		return nil, errNotPrepared
	case C.NextKeyRangeResult_None:
		return nil, nil
	case C.NextKeyRangeResult_Some:
		return newNextKeyRange(*(*C.NextKeyRange)(unsafe.Pointer(&result.anon0))), nil
	case C.NextKeyRangeResult_Err:
		return nil, newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
	default:
		return nil, fmt.Errorf("unknown C.NextKeyRangeResult tag: %d", result.tag)
	}
}

func getRangeProofFromRangeProofResult(result C.RangeProofResult) (*RangeProof, error) {
	switch result.tag {
	case C.RangeProofResult_NullHandlePointer:
		return nil, errDBClosed
	case C.RangeProofResult_RevisionNotFound:
		// NOTE: the result value contains the provided root hash, we could use
		// it in the error message if needed.
		return nil, errRevisionNotFound
	case C.RangeProofResult_EmptyTrie:
		return nil, errEmptyTrie
	case C.RangeProofResult_Ok:
		ptr := *(**C.RangeProofContext)(unsafe.Pointer(&result.anon0))
		return &RangeProof{handle: ptr}, nil
	case C.RangeProofResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.RangeProofResult tag: %d", result.tag)
	}
}

func getChangeProofFromChangeProofResult(result C.ChangeProofResult) (*ChangeProof, error) {
	switch result.tag {
	case C.ChangeProofResult_NullHandlePointer:
		return nil, errDBClosed
	case C.ChangeProofResult_Ok:
		ptr := *(**C.ChangeProofContext)(unsafe.Pointer(&result.anon0))
		return &ChangeProof{handle: ptr}, nil
	case C.ChangeProofResult_Err:
		err := newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
		return nil, err
	default:
		return nil, fmt.Errorf("unknown C.ChangeProofResult tag: %d", result.tag)
	}
}

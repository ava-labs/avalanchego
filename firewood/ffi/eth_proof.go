// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

// #include <stdlib.h>
// #include "firewood.h"
// #cgo noescape fwd_eth_get_proof
// #cgo nocallback fwd_eth_get_proof
// #cgo noescape fwd_eth_get_proof_on_reconstructed
// #cgo nocallback fwd_eth_get_proof_on_reconstructed
// #cgo noescape fwd_free_eth_proof
// #cgo nocallback fwd_free_eth_proof
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

// ErrEthProofNotSupported is returned by [Revision.EthGetProof] and
// [Reconstructed.EthGetProof] when the database is not running in Ethereum hash
// mode and therefore cannot produce an eth_getProof-compatible proof.
var ErrEthProofNotSupported = errors.New("eth_getProof requires ethereum hash mode")

// EthAccountProof is the result of [Revision.EthGetProof] and
// [Reconstructed.EthGetProof]: the four account scalars plus the RLP-encoded
// account-trie nodes proving them, and one [EthStorageProof] per requested
// storage slot.
//
// The proof byte slices are canonical RLP-encoded Ethereum MPT nodes, suitable
// for verification with go-ethereum's trie.VerifyProof against the revision
// root (account proof) or against StorageHash (storage proofs). JSON
// marshalling that matches the eth_getProof JSON-RPC response is left to the
// caller.
type EthAccountProof struct {
	// Nonce is the account transaction count.
	Nonce uint64
	// Balance is the account balance, zero-padded big-endian.
	Balance [32]byte
	// CodeHash is the Keccak-256 of the account's contract code (the empty-code
	// hash for accounts with no code).
	CodeHash [32]byte
	// StorageHash is the storage trie root embedded in the account leaf (the
	// empty-trie root for absent accounts).
	StorageHash [32]byte
	// AccountProof holds the RLP-encoded account-trie nodes, root-to-leaf order.
	AccountProof [][]byte
	// StorageProof holds one proof per requested slot key, in input order.
	StorageProof []EthStorageProof
}

// EthStorageProof is a single per-slot entry within an [EthAccountProof].
type EthStorageProof struct {
	// Key is the requested 32-byte slot trie key, echoed back from the input.
	Key [32]byte
	// Value is the stored slot value, or nil for an exclusion proof.
	Value []byte
	// Proof holds the RLP-encoded storage-trie nodes, root-to-leaf order. It is
	// empty when the account has no storage at all.
	Proof [][]byte
}

// EthGetProof produces an eth_getProof-compatible proof for the account at
// accountKey and each storage slot in slotKeys, evaluated against this
// revision.
//
// Both accountKey and every entry of slotKeys must be 32-byte trie keys: the
// caller is responsible for keccak-hashing addresses and storage slots into
// their trie-key forms (firewood stores accounts at keccak256(address) and
// slots at keccak256(address) ++ keccak256(slot)).
//
// Absent accounts come back with zero account scalars plus the empty-code and
// empty-trie hashes; the proof bytes themselves distinguish inclusion from
// exclusion to a verifier.
//
// It returns [ErrEthProofNotSupported] if the database is not in Ethereum hash
// mode, and [ErrDroppedRevision] if this revision has already been released.
func (r *Revision) EthGetProof(accountKey []byte, slotKeys [][]byte) (*EthAccountProof, error) {
	r.lease.mu.RLock()
	defer r.lease.mu.RUnlock()
	if r.dropped {
		return nil, ErrDroppedRevision
	}

	return ethGetProofCall(accountKey, slotKeys, func(a C.BorrowedBytes, s C.BorrowedBytes2D) C.EthProofResult {
		return C.fwd_eth_get_proof(r.ptr, a, s)
	})
}

// ethGetProofCall marshals the keys (pinned for the duration of call), invokes
// call with the resulting borrowed views, and decodes the result. call must
// perform exactly one fwd_eth_get_proof* invocation.
func ethGetProofCall(
	accountKey []byte,
	slotKeys [][]byte,
	call func(C.BorrowedBytes, C.BorrowedBytes2D) C.EthProofResult,
) (*EthAccountProof, error) {
	var pinner runtime.Pinner
	defer pinner.Unpin()

	cAccountKey := newBorrowedBytes(accountKey, &pinner)
	cStorageKeys := newBorrowedBytes2D(slotKeys, &pinner)

	return getEthProofFromResult(call(cAccountKey, cStorageKeys))
}

// EthGetProof is [Revision.EthGetProof] evaluated against this reconstructed
// view. It returns [ErrEthProofNotSupported] if the database is not in Ethereum
// hash mode, and [ErrDroppedReconstructed] if the view has been released.
func (r *Reconstructed) EthGetProof(accountKey []byte, slotKeys [][]byte) (*EthAccountProof, error) {
	r.lease.mu.RLock()
	defer r.lease.mu.RUnlock()
	if r.dropped {
		return nil, ErrDroppedReconstructed
	}

	return ethGetProofCall(accountKey, slotKeys, func(a C.BorrowedBytes, s C.BorrowedBytes2D) C.EthProofResult {
		return C.fwd_eth_get_proof_on_reconstructed(r.ptr, a, s)
	})
}

// newBorrowedBytes2D builds a BorrowedBytes2D referencing each slice in slices.
//
// The backing array and every inner slice are pinned for the lifetime of the
// provided pinner, which must outlive the C call that consumes the result.
func newBorrowedBytes2D(slices [][]byte, pinner *runtime.Pinner) C.BorrowedBytes2D {
	if len(slices) == 0 {
		return C.BorrowedBytes2D{ptr: nil, len: 0}
	}

	cSlices := make([]C.BorrowedBytes, len(slices))
	for i, s := range slices {
		cSlices[i] = newBorrowedBytes(s, pinner)
	}

	ptr := unsafe.SliceData(cSlices)
	pinner.Pin(ptr)

	return C.BorrowedBytes2D{
		ptr: ptr,
		len: C.size_t(len(slices)),
	}
}

// getEthProofFromResult converts a C.EthProofResult into an EthAccountProof.
//
// On success it deep-copies every buffer out of Rust memory and frees the
// entire EthProofOwned with a single fwd_free_eth_proof call, so the returned
// value is independent of the revision and database.
func getEthProofFromResult(result C.EthProofResult) (*EthAccountProof, error) {
	switch result.tag {
	case C.EthProofResult_NullHandlePointer:
		return nil, errDBClosed
	case C.EthProofResult_NotSupported:
		return nil, ErrEthProofNotSupported
	case C.EthProofResult_Ok:
		ptr := *(**C.EthProofOwned)(unsafe.Pointer(&result.anon0))
		proof := copyEthProof(ptr)
		if err := getErrorFromVoidResult(C.fwd_free_eth_proof(ptr)); err != nil {
			return nil, fmt.Errorf("%w: %w", errFreeingValue, err)
		}
		return proof, nil
	case C.EthProofResult_Err:
		return nil, newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
	default:
		return nil, fmt.Errorf("unknown C.EthProofResult tag: %d", result.tag)
	}
}

// copyEthProof deep-copies an owned proof out of Rust memory into Go-owned
// types. It does not free the owned proof; the caller is responsible for that.
func copyEthProof(owned *C.EthProofOwned) *EthAccountProof {
	proof := &EthAccountProof{
		Nonce:        uint64(owned.nonce),
		Balance:      *(*[32]byte)(unsafe.Pointer(&owned.balance)),
		CodeHash:     *(*[32]byte)(unsafe.Pointer(&owned.code_hash)),
		StorageHash:  *(*[32]byte)(unsafe.Pointer(&owned.storage_hash)),
		AccountProof: copyOwnedBytesSlice(owned.account_proof),
	}

	storageProofs := unsafe.Slice(owned.storage_proofs.ptr, owned.storage_proofs.len)
	proof.StorageProof = make([]EthStorageProof, len(storageProofs))
	for i := range storageProofs {
		sp := &storageProofs[i]
		entry := EthStorageProof{
			Key:   *(*[32]byte)(unsafe.Pointer(&sp.key)),
			Proof: copyOwnedBytesSlice(sp.proof),
		}
		if sp.value.tag == C.Maybe_OwnedBytes_Some_OwnedBytes {
			value := *(*C.OwnedBytes)(unsafe.Pointer(&sp.value.anon0))
			entry.Value = newOwnedBytes(value).CopiedBytes()
		}
		proof.StorageProof[i] = entry
	}

	return proof
}

// copyOwnedBytesSlice deep-copies an OwnedSlice<OwnedBytes> into a [][]byte.
func copyOwnedBytesSlice(slice C.OwnedSlice_OwnedBytes) [][]byte {
	if slice.ptr == nil || slice.len == 0 {
		return nil
	}

	entries := unsafe.Slice(slice.ptr, slice.len)
	out := make([][]byte, len(entries))
	for i := range entries {
		out[i] = newOwnedBytes(entries[i]).CopiedBytes()
	}
	return out
}

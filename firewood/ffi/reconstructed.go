// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

// #include <stdlib.h>
// #include "firewood.h"
// #cgo noescape fwd_reconstructed_root_hash
// #cgo nocallback fwd_reconstructed_root_hash
// #cgo noescape fwd_get_from_reconstructed
// #cgo nocallback fwd_get_from_reconstructed
// #cgo noescape fwd_iter_on_reconstructed
// #cgo nocallback fwd_iter_on_reconstructed
// #cgo noescape fwd_reconstruct_on_reconstructed
// #cgo nocallback fwd_reconstruct_on_reconstructed
// #cgo noescape fwd_clone_reconstructed
// #cgo nocallback fwd_clone_reconstructed
// #cgo noescape fwd_reconstructed_dump
// #cgo nocallback fwd_reconstructed_dump
// #cgo noescape fwd_free_reconstructed
// #cgo nocallback fwd_free_reconstructed
import "C"

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// ErrDroppedReconstructed wraps [ErrDropped].
var ErrDroppedReconstructed = fmt.Errorf("reconstructed view %w", ErrDropped)

// Lock ordering for Reconstructed:
//
//	lease.mu (A)  before  rootMu (B)
//
// Every method that acquires both locks does so in A-then-B order:
//
//	Root()        — A.RLock  → B.Lock
//	Reconstruct() — A.Lock   → B.Lock
//
// Methods that acquire only one lock (Get, Iter, Dump take A.RLock;
// Drop takes A.Lock via disown) cannot participate in an AB/BA cycle.
//
// Cross-type: Reconstructed never touches commitLock, so there is no
// ordering constraint with Proposal.Commit or Database.Close beyond the
// keep-alive registry's outstanding-handle count.

// Reconstructed is a linear, read-only reconstructed view over a historical
// revision.
//
// Unlike [Proposal], a Reconstructed view cannot be committed and does not
// participate in proposal branching semantics. Calling [Reconstructed.Reconstruct]
// updates this instance in place.
//
// Reconstructed handles must be released before the associated database is closed
// by calling [Reconstructed.Drop]. A cleanup function is registered to call Drop
// automatically if needed, but explicit calls are recommended.
type Reconstructed struct {
	*handle[*C.ReconstructedHandle]

	rootMu  sync.Mutex
	root    Hash
	rootSet bool
}

// Root returns the root hash of the reconstructed view.
// Unlike other methods, Root remains usable after [Reconstructed.Drop] and
// returns the last cached root (or [EmptyRoot] if the root was never computed).
func (r *Reconstructed) Root() Hash {
	// Lock order: lease.mu before rootMu, matching Reconstruct().
	r.lease.mu.RLock()
	defer r.lease.mu.RUnlock()

	r.rootMu.Lock()
	defer r.rootMu.Unlock()

	if r.rootSet {
		return r.root
	}

	if r.dropped {
		return EmptyRoot
	}

	result := C.fwd_reconstructed_root_hash(r.ptr)
	root, err := getHashKeyFromHashResult(result)
	if err != nil {
		return EmptyRoot
	}

	r.root = root
	r.rootSet = true
	return r.root
}

// Get retrieves the value for the given key in this reconstructed view.
func (r *Reconstructed) Get(key []byte) ([]byte, error) {
	r.lease.mu.RLock()
	defer r.lease.mu.RUnlock()

	if r.dropped {
		return nil, ErrDroppedReconstructed
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	return getValueFromValueResult(C.fwd_get_from_reconstructed(
		r.ptr,
		newBorrowedBytes(key, &pinner),
	))
}

// Iter creates an iterator over the reconstructed view.
func (r *Reconstructed) Iter(key []byte) (*Iterator, error) {
	r.lease.mu.RLock()
	defer r.lease.mu.RUnlock()

	if r.dropped {
		return nil, ErrDroppedReconstructed
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	itResult := C.fwd_iter_on_reconstructed(r.ptr, newBorrowedBytes(key, &pinner))
	return getIteratorFromIteratorResult(itResult, r.lease.registry)
}

// Reconstruct applies a new batch on top of this reconstructed view.
//
// On success, the receiver is updated to point at the newly reconstructed view.
// On error, the receiver is no longer usable and its resources are fully released;
// calling [Reconstructed.Drop] is not required (but safe as a no-op).
func (r *Reconstructed) Reconstruct(batch []BatchOp) error {
	r.lease.mu.Lock()
	defer r.lease.mu.Unlock()

	if r.dropped {
		return ErrDroppedReconstructed
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()
	kvp := newKeyValuePairsFromBatch(batch, &pinner)

	result := C.fwd_reconstruct_on_reconstructed(r.ptr, kvp)
	// The old handle is consumed by the FFI call regardless of outcome.
	r.ptr = nil

	newHandle, err := decodeReconstructedResult(result)
	if err != nil {
		// The old handle was consumed by the FFI call, so mark as dropped
		// and disown the keep-alive lease so Database.Close is not blocked.
		r.dropped = true
		// We already hold r.lease.mu (Lock) for the wider critical
		// section, so use releaseLocked to avoid re-entering mu.
		r.lease.releaseLocked()
		return err
	}

	r.ptr = newHandle

	// Lock order matches Root(): lease.mu (already held) before rootMu.
	r.rootMu.Lock()
	r.root = EmptyRoot
	r.rootSet = false
	r.rootMu.Unlock()

	return nil
}

// Clone returns an independent [Reconstructed] handle that shares the
// underlying view with the receiver. The two handles can be used, reconstructed,
// and freed independently.
func (r *Reconstructed) Clone() (*Reconstructed, error) {
	r.lease.mu.RLock()
	defer r.lease.mu.RUnlock()

	if r.dropped {
		return nil, ErrDroppedReconstructed
	}

	result := C.fwd_clone_reconstructed(r.ptr)
	return getReconstructedFromResult(result, r.lease.registry)
}

// Dump returns a DOT (Graphviz) representation of this reconstructed view.
func (r *Reconstructed) Dump() (string, error) {
	r.lease.mu.RLock()
	defer r.lease.mu.RUnlock()

	if r.dropped {
		return "", ErrDroppedReconstructed
	}

	bytes, err := getValueFromValueResult(C.fwd_reconstructed_dump(r.ptr))
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

// decodeReconstructedResult unwraps a C.ReconstructedResult to the raw
// C handle, converting tag-encoded variants into Go errors. Used both by
// the wrapping constructor [getReconstructedFromResult] and by
// [Reconstructed.Reconstruct], which swaps the inner pointer without
// allocating a new wrapper.
func decodeReconstructedResult(result C.ReconstructedResult) (*C.ReconstructedHandle, error) {
	switch result.tag {
	case C.ReconstructedResult_NullHandlePointer:
		return nil, errDBClosed
	case C.ReconstructedResult_Ok:
		body := (*C.ReconstructedResult_Ok_Body)(unsafe.Pointer(&result.anon0))
		return body.handle, nil
	case C.ReconstructedResult_Err:
		return nil, newOwnedBytes(*(*C.OwnedBytes)(unsafe.Pointer(&result.anon0))).intoError()
	default:
		return nil, fmt.Errorf("unknown C.ReconstructedResult tag: %d", result.tag)
	}
}

func getReconstructedFromResult(result C.ReconstructedResult, registry *keepAliveRegistry) (*Reconstructed, error) {
	cHandle, err := decodeReconstructedResult(result)
	if err != nil {
		return nil, err
	}
	reconstructed := &Reconstructed{
		handle: newHandle(cHandle, func(h *C.ReconstructedHandle) C.VoidResult {
			return C.fwd_free_reconstructed(h)
		}),
		root: EmptyRoot,
	}
	// attach calls reconstructed.Drop on the closed-registry path; nothing else to clean up.
	if err := reconstructed.lease.attach(registry, reconstructed.Drop); err != nil {
		return nil, err
	}
	runtime.AddCleanup(reconstructed, drop[*C.ReconstructedHandle], reconstructed.handle)
	return reconstructed, nil
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"

	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

type DB interface {
	merkledb.Clearer
	merkledb.MerkleRootGetter
	merkledb.ProofGetter
	merkledb.ChangeProofer
	merkledb.RangeProofer
}

// A `Proof` is a stateful object that represents a proof returned from a remote peer.
// It must be verified before being committed to the underlying database.
// The `Proof` is backed by a database, which may be altered during Commit.
// Since the `Proof` must be returned by a `ProofParser`, it has knowledge of the request
// that generated it, which will be used when finding the next key to request.
type Proof interface {
	// Verify checks the validity of the proof. If the proof is invalid,
	// an error is returned and the proof should not be committed, and
	// should be discarded and re-requested.
	Verify(context.Context) error

	// Commit applies the changes in the proof to the underlying database.
	// Any proof should be verified before being committed.
	// An error is returned if the commit fails, in which case the DB may be corrupted.
	Commit(context.Context) error

	// FindNextKey returns the next key that should be requested after this proof
	// is applied. If there are no more keys to request (so the root hash is up to date),
	// it returns Nothing.
	FindNextKey(context.Context) (maybe.Maybe[[]byte], error)
}

type ProofParser interface {
	ParseRangeProof(responseBytes, rootHash []byte, startKey, endKey maybe.Maybe[[]byte], keyLimit uint32) (Proof, error)
	ParseChangeProof(responseBytes, rootHash []byte, startKey, endKey maybe.Maybe[[]byte], keyLimit uint32) (Proof, error)
}

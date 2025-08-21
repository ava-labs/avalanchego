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
	// Commit applies the changes in the proof to the underlying database.
	// An error is returned if the commit fails, in which case the DB may be
	// corrupted. The byte slice returned is the next key that should be
	// requested. If Nothing is returned, there are no more keys to request
	// before the end key of the request.
	Commit(context.Context) (maybe.Maybe[[]byte], error)
}

type ProofParser interface {
	ParseRangeProof(ctx context.Context, responseBytes, rootHash []byte, startKey, endKey maybe.Maybe[[]byte], keyLimit uint32) (Proof, error)
	ParseChangeProof(ctx context.Context, responseBytes, rootHash []byte, startKey, endKey maybe.Maybe[[]byte], keyLimit uint32) (Proof, error)
}

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

type ProofHandler[T, U any] interface {
	ParseRangeProof(ctx context.Context, responseBytes, rootHash []byte, startKey, endKey maybe.Maybe[[]byte], keyLimit uint32) (T, error)
	ParseChangeProof(ctx context.Context, responseBytes, rootHash []byte, startKey, endKey maybe.Maybe[[]byte], keyLimit uint32) (U, error)
	CommitRangeProof(ctx context.Context, proof T) (maybe.Maybe[[]byte], error)
	CommitChangeProof(ctx context.Context, proof U) (maybe.Maybe[[]byte], error)
}

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

type ProofCreator interface {
	RangeProof(ctx context.Context, root []byte, start, end maybe.Maybe[[]byte], keyLimit, byteLimit int) ([]byte, error)
	ChangeProof(ctx context.Context, startRoot, endRoot []byte, start, end maybe.Maybe[[]byte], keyLimit, byteLimit int) ([]byte, error)
}

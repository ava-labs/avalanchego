// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"

	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

type DB interface {
	merkledb.Clearer
	merkledb.MerkleRootGetter
	merkledb.ProofGetter
	merkledb.ChangeProofer
	merkledb.RangeProofer
}

type Proof interface {
	Verify(context.Context) error
	Commit(context.Context) error
	FindNextKey(context.Context) (maybe.Maybe[[]byte], error)
}

type ProofParser interface {
	ParseRangeProof(request *pb.SyncGetRangeProofRequest, responseBytes []byte) (Proof, error)
	ParseChangeProof(request *pb.SyncGetChangeProofRequest, responseBytes []byte) (Proof, error)
}

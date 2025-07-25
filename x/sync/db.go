// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
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

type DBClient interface {
	HandleRangeProofResponse(ctx context.Context, request *pb.SyncGetRangeProofRequest, responseBytes []byte) (maybe.Maybe[[]byte], error)
	HandleChangeProofResponse(
		ctx context.Context,
		request *pb.SyncGetChangeProofRequest,
		responseBytes []byte,
	) (maybe.Maybe[[]byte], error)
	FinalizeSync(ctx context.Context, root ids.ID) error
	Error() error
}

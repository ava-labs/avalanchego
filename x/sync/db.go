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
	// Given the range proof in bytes from a peer, verifies and commits the proof. It returns any error encountered
	// during the verification or commit process, as well as the next key to be requested from a peer.
	HandleRangeProofResponse(ctx context.Context, request *pb.SyncGetRangeProofRequest, responseBytes []byte) (maybe.Maybe[[]byte], error)

	// Given the change proof in bytes from a peer, verifies and commits the proof. It returns any error encountered
	// during the verification or commit process, as well as the next key to be requested from a peer.
	HandleChangeProofResponse(
		ctx context.Context,
		request *pb.SyncGetChangeProofRequest,
		responseBytes []byte,
	) (maybe.Maybe[[]byte], error)

	// Finalizes the sync process by ensuring the provided root is the current root of the database, and any
	// additional cleanup is performed.
	// If the root is empty, it clears the database.
	FinalizeSync(ctx context.Context, root ids.ID) error

	// If any unrecoverable error occurs during the sync process within the database,
	// the error is available in this method.
	Error() error
}

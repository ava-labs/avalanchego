// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

type DBSyncClient interface {
	GetRootHash(ctx context.Context) (ids.ID, error)
	HandleRangeProofResponse(ctx context.Context, request *pb.SyncGetRangeProofRequest, responseBytes []byte) (maybe.Maybe[[]byte], error)
	HandleChangeProofResponse(
		ctx context.Context,
		request *pb.SyncGetChangeProofRequest,
		responseBytes []byte,
	) (maybe.Maybe[[]byte], error)
	Error() error
	Clear() error
}

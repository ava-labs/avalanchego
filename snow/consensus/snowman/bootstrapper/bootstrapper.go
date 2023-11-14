// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

type Bootstrapper interface {
	GetAcceptedFrontiersToSend(ctx context.Context) (peers set.Set[ids.NodeID])
	RecordAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, blkIDs ...ids.ID)
	GetAcceptedFrontier(ctx context.Context) (blkIDs []ids.ID, finalized bool)

	GetAcceptedToSend(ctx context.Context) (peers set.Set[ids.NodeID])
	RecordAccepted(ctx context.Context, nodeID ids.NodeID, blkIDs []ids.ID) error
	GetAccepted(ctx context.Context) (blkIDs []ids.ID, finalized bool)
}

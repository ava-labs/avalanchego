// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

type Poll interface {
	// GetPeers returns the set of peers whose opinion should be requested. It
	// is expected to repeatedly call this function along with [RecordOpinion]
	// until [Result] returns finalized.
	GetPeers(ctx context.Context) (peers set.Set[ids.NodeID])
	// RecordOpinion of a node whose opinion was requested.
	RecordOpinion(ctx context.Context, nodeID ids.NodeID, blkIDs set.Set[ids.ID]) error
	// Result returns the evaluation of all the peer's opinions along with a
	// flag to identify that the result has finished being calculated.
	Result(ctx context.Context) (blkIDs []ids.ID, finalized bool)
}

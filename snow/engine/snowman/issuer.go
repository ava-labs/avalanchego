// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/job"
)

var _ job.Job[ids.ID] = (*issuer)(nil)

// issuer issues [blk] into to consensus after its dependencies are met.
type issuer struct {
	e            *Engine
	nodeID       ids.NodeID // nodeID of the peer that provided this block
	blk          snowman.Block
	push         bool
	issuedMetric prometheus.Counter
}

func (i *issuer) Execute(ctx context.Context, _ []ids.ID, abandoned []ids.ID) error {
	if len(abandoned) == 0 {
		// If the parent block wasn't abandoned, this block can be issued.
		return i.e.deliver(ctx, i.nodeID, i.blk, i.push, i.issuedMetric)
	}

	// If the parent block was abandoned, this block should be abandoned as
	// well.
	blkID := i.blk.ID()
	delete(i.e.pending, blkID)
	i.e.markAsUnverified(i.blk)
	return i.e.blocked.Abandon(ctx, blkID)
}

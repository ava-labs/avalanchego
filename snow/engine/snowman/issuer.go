// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/set"
)

// issuer issues [blk] into to consensus after its dependencies are met.
type issuer struct {
	t            *Transitive
	nodeID       ids.NodeID // nodeID of the peer that provided this block
	blk          snowman.Block
	issuedMetric prometheus.Counter
	abandoned    bool
	deps         set.Set[ids.ID]
	push         bool
}

func (i *issuer) Dependencies() set.Set[ids.ID] {
	return i.deps
}

// Mark that a dependency has been met
func (i *issuer) Fulfill(ctx context.Context, id ids.ID) {
	i.deps.Remove(id)
	i.Update(ctx)
}

// Abandon the attempt to issue [i.block]
func (i *issuer) Abandon(ctx context.Context, _ ids.ID) {
	if !i.abandoned {
		blkID := i.blk.ID()
		i.t.removeFromPending(i.blk)
		i.t.addToNonVerifieds(i.blk)
		i.t.blocked.Abandon(ctx, blkID)
	}
	i.abandoned = true
}

func (i *issuer) Update(ctx context.Context) {
	if i.abandoned || i.deps.Len() != 0 || i.t.errs.Errored() {
		return
	}
	// Issue the block into consensus
	i.t.errs.Add(i.t.deliver(ctx, i.nodeID, i.blk, i.push, i.issuedMetric))
}

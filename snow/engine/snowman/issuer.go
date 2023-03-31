// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/set"
)

// issuer issues [blk] into to consensus after its dependencies are met.
type issuer struct {
	t         *Transitive
	blk       snowman.Block
	abandoned bool
	deps      set.Set[ids.ID]
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

		// Tracks performance statistics
		i.t.metrics.numRequests.Set(float64(i.t.blkReqs.Len()))
		i.t.metrics.numBlocked.Set(float64(len(i.t.pending)))
		i.t.metrics.numBlockers.Set(float64(i.t.blocked.Len()))
	}
	i.abandoned = true
}

func (i *issuer) Update(ctx context.Context) {
	if i.abandoned || i.deps.Len() != 0 || i.t.errs.Errored() {
		return
	}
	// Issue the block into consensus
	i.t.errs.Add(i.t.deliver(ctx, i.blk))
}

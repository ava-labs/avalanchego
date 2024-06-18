// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

// issuer issues [blk] into to consensus after its dependencies are met.
type issuer struct {
	t            *Transitive
	nodeID       ids.NodeID // nodeID of the peer that provided this block
	blk          snowman.Block
	push         bool
	issuedMetric prometheus.Counter
}

func (i *issuer) Execute(ctx context.Context) error {
	return i.t.deliver(ctx, i.nodeID, i.blk, i.push, i.issuedMetric)
}

func (i *issuer) Cancel(ctx context.Context) error {
	blkID := i.blk.ID()
	i.t.removeFromPending(i.blk)
	i.t.addToNonVerifieds(i.blk)
	return i.t.blocked.Abandon(ctx, blkID)
}

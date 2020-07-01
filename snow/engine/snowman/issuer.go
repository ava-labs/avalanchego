// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
)

type issuer struct {
	t         *Transitive
	blk       snowman.Block
	abandoned bool
	deps      ids.Set
}

func (i *issuer) Dependencies() ids.Set { return i.deps }

func (i *issuer) Fulfill(id ids.ID) {
	i.deps.Remove(id)
	i.Update()
}

func (i *issuer) Abandon(ids.ID) {
	if !i.abandoned {
		blkID := i.blk.ID()
		i.t.pending.Remove(blkID)
		i.t.blocked.Abandon(blkID)

		// Tracks performance statistics
		i.t.numBlkRequests.Set(float64(i.t.blkReqs.Len()))
		i.t.numBlockedBlk.Set(float64(i.t.pending.Len()))
	}
	i.abandoned = true
}

func (i *issuer) Update() {
	if i.abandoned || i.deps.Len() != 0 || i.t.errs.Errored() {
		return
	}

	i.t.errs.Add(i.t.deliver(i.blk))
}

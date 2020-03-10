// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
)

type convincer struct {
	consensus snowman.Consensus
	sender    common.Sender
	vdr       ids.ShortID
	requestID uint32
	abandoned bool
	deps      ids.Set
}

func (c *convincer) Dependencies() ids.Set { return c.deps }

func (c *convincer) Fulfill(id ids.ID) {
	c.deps.Remove(id)
	c.Update()
}

func (c *convincer) Abandon(ids.ID) { c.abandoned = true }

func (c *convincer) Update() {
	if c.abandoned || c.deps.Len() != 0 {
		return
	}

	pref := c.consensus.Preference()
	prefSet := ids.Set{}
	prefSet.Add(pref)
	c.sender.Chits(c.vdr, c.requestID, prefSet)
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"
)

type connections struct {
	lock  sync.Mutex
	nodes map[ids.NodeID]*version.Application
}

func (c *connections) add(nodeID ids.NodeID, v *version.Application) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.nodes[nodeID] = v
}

func (c *connections) remove(nodeID ids.NodeID) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.nodes, nodeID)
}

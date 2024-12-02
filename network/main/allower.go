package main

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/subnets"
)

var _ subnets.Allower = &alwaysAllower{}

type alwaysAllower struct{}

// allow messages to any node
func (alwaysAllower) IsAllowed(nodeID ids.NodeID, isValidator bool) bool {
	return true
}

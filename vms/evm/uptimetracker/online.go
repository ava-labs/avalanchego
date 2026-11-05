// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// connector reports node connectivity to the uptime manager.
type connector interface {
	Connect(nodeID ids.NodeID) error
	Disconnect(nodeID ids.NodeID) error
}

// onlineTracker reports nodes to the uptime manager as connected only while
// they are connected peers and not inactive validators.
type onlineTracker struct {
	manager connector
	state   *state

	connected set.Set[ids.NodeID]
	// online is the set of nodes last reported to manager as connected.
	online set.Set[ids.NodeID]
}

func (o *onlineTracker) connect(nodeID ids.NodeID) error {
	o.connected.Add(nodeID)
	return o.reconcile(nodeID)
}

func (o *onlineTracker) disconnect(nodeID ids.NodeID) error {
	o.connected.Remove(nodeID)
	return o.reconcile(nodeID)
}

// reconcileAll MUST be called after the validator set changes.
func (o *onlineTracker) reconcileAll() error {
	for nodeID := range o.connected {
		if err := o.reconcile(nodeID); err != nil {
			return err
		}
	}
	return nil
}

// reconcile reports nodeID to the manager if its online status differs from
// what was last reported.
func (o *onlineTracker) reconcile(nodeID ids.NodeID) error {
	shouldBeOnline := o.connected.Contains(nodeID) && !o.state.isInactiveValidator(nodeID)
	isOnline := o.online.Contains(nodeID)
	switch {
	case shouldBeOnline && !isOnline:
		o.online.Add(nodeID)
		return o.manager.Connect(nodeID)
	case !shouldBeOnline && isOnline:
		o.online.Remove(nodeID)
		return o.manager.Disconnect(nodeID)
	}
	return nil
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/set"
)

type pausableManager struct {
	manager    uptime.Manager
	pausedVdrs set.Set[ids.NodeID]
	// connectedVdrs is a set of nodes that are connected to the manager.
	// This is used to immediately connect nodes when they are unpaused.
	connectedVdrs set.Set[ids.NodeID]
}

// NewPausableManager takes an uptime.Manager and returns a PausableManager
func newPausableManager(manager uptime.Manager) *pausableManager {
	return &pausableManager{
		manager:       manager,
		pausedVdrs:    make(set.Set[ids.NodeID]),
		connectedVdrs: make(set.Set[ids.NodeID]),
	}
}

// Connect connects the node with the given ID to the uptime.Manager
// If the node is paused, it will not be connected
//
// The AvalancheGo uptime manager records the time when a peer is connected to the tracker node.
// When a paused/`inactive` validator is connected, the pausable uptime manager does not directly
// invoke the `Connected` method on the AvalancheGo uptime manager, thus the connection time is not
// directly recorded. Instead, the pausable uptime manager waits for the validator to increase its
// continuous validation fee balance and resume operation. When the validator resumes, the tracker
// node records the resumed time and starts tracking the uptime of the validator.
//
// Note: The uptime manager does not check if the connected peer is a validator or not. It records
// the connection time assuming that a non-validator peer can become a validator whilst they're
// connected to the uptime manager.
func (p *pausableManager) connect(nodeID ids.NodeID) error {
	p.connectedVdrs.Add(nodeID)
	if !p.isPaused(nodeID) && !p.manager.IsConnected(nodeID) {
		return p.manager.Connect(nodeID)
	}
	return nil
}

// IsConnected returns true if the node with the given ID is connected to this manager
// Note: Inner manager may have a different view of the connection status due to pausing
func (p *pausableManager) IsConnected(nodeID ids.NodeID) bool {
	return p.connectedVdrs.Contains(nodeID)
}

// Disconnect disconnects the node with the given ID from the uptime.Manager
// If the node is paused, it will not be disconnected
// Invariant: we should never have a connected paused node that is disconnecting
//
// When a peer validator is disconnected, the AvalancheGo uptime manager updates the uptime of the
// validator by adding the duration between the connection time and the disconnection time to the
// uptime of the validator. When a validator is paused/`inactive`, the pausable uptime manager
// handles the `inactive` peers as if they were disconnected. Thus the uptime manager assumes that
// no paused peers can be disconnected again from the pausable uptime manager.
func (p *pausableManager) disconnect(nodeID ids.NodeID) error {
	p.connectedVdrs.Remove(nodeID)
	if p.manager.IsConnected(nodeID) {
		return p.manager.Disconnect(nodeID)
	}
	return nil
}

// OnValidatorAdded is called when a validator is added.
// If the node is inactive, it will be paused.
func (p *pausableManager) onValidatorAdded(_ ids.ID, nodeID ids.NodeID, _ uint64, isActive bool) {
	if !isActive {
		err := p.pause(nodeID)
		if err != nil {
			log.Error("failed to handle added validator %s: %s", nodeID, err)
		}
	}
}

// OnValidatorRemoved is called when a validator is removed.
// If the node is already paused, it will be resumed.
func (p *pausableManager) onValidatorRemoved(_ ids.ID, nodeID ids.NodeID) {
	if p.isPaused(nodeID) {
		err := p.resume(nodeID)
		if err != nil {
			log.Error("failed to handle validator removed %s: %s", nodeID, err)
		}
	}
}

// OnValidatorStatusUpdated is called when the status of a validator is updated.
// If the node is active, it will be resumed. If the node is inactive, it will be paused.
func (p *pausableManager) onValidatorStatusUpdated(_ ids.ID, nodeID ids.NodeID, isActive bool) {
	var err error
	if isActive {
		err = p.resume(nodeID)
	} else {
		err = p.pause(nodeID)
	}
	if err != nil {
		log.Error("failed to update status for node %s: %s", nodeID, err)
	}
}

// IsPaused returns true if the node with the given ID is paused.
func (p *pausableManager) isPaused(nodeID ids.NodeID) bool {
	return p.pausedVdrs.Contains(nodeID)
}

// pause pauses uptime tracking for the node with the given ID
// pause can disconnect the node from the uptime.Manager if it is connected.
//
// The pausable uptime manager can listen for validator status changes by subscribing to the state.
// When the state invokes the `OnValidatorStatusChange` method, the pausable uptime manager pauses
// the uptime tracking of the validator if the validator is currently `inactive`. When a validator
// is paused, it is treated as if it is disconnected from the tracker node; thus, its uptime is
// updated from the connection time to the pause time, and uptime manager stops tracking the
// uptime of the validator.
func (p *pausableManager) pause(nodeID ids.NodeID) error {
	p.pausedVdrs.Add(nodeID)
	if p.manager.IsConnected(nodeID) {
		// If the node is connected, then we need to disconnect it from
		// manager
		// This should be fine in case tracking has not started yet since
		// the inner manager should handle disconnects accordingly
		return p.manager.Disconnect(nodeID)
	}
	return nil
}

// resume resumes uptime tracking for the node with the given ID
// resume can connect the node to the uptime.Manager if it was connected.
//
// When a paused validator peer resumes, meaning its status becomes `active`, the pausable uptime
// manager resumes the uptime tracking of the validator. It treats the peer as if it is connected
// to the tracker node.
func (p *pausableManager) resume(nodeID ids.NodeID) error {
	p.pausedVdrs.Remove(nodeID)
	if p.connectedVdrs.Contains(nodeID) && !p.manager.IsConnected(nodeID) {
		return p.manager.Connect(nodeID)
	}
	return nil
}

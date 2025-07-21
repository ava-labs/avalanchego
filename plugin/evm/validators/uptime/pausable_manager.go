// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"

	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/subnet-evm/plugin/evm/validators/uptime/interfaces"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/set"
)

var errPausedDisconnect = errors.New("paused node cannot be disconnected")

type pausableManager struct {
	uptime.Manager
	pausedVdrs set.Set[ids.NodeID]
	// connectedVdrs is a set of nodes that are connected to the manager.
	// This is used to immediately connect nodes when they are unpaused.
	connectedVdrs set.Set[ids.NodeID]
}

// NewPausableManager takes an uptime.Manager and returns a PausableManager
func NewPausableManager(manager uptime.Manager) interfaces.PausableManager {
	return &pausableManager{
		pausedVdrs:    make(set.Set[ids.NodeID]),
		connectedVdrs: make(set.Set[ids.NodeID]),
		Manager:       manager,
	}
}

// Connect connects the node with the given ID to the uptime.Manager
// If the node is paused, it will not be connected
func (p *pausableManager) Connect(nodeID ids.NodeID) error {
	p.connectedVdrs.Add(nodeID)
	if !p.IsPaused(nodeID) && !p.Manager.IsConnected(nodeID) {
		return p.Manager.Connect(nodeID)
	}
	return nil
}

// Disconnect disconnects the node with the given ID from the uptime.Manager
// If the node is paused, it will not be disconnected
// Invariant: we should never have a connected paused node that is disconnecting
func (p *pausableManager) Disconnect(nodeID ids.NodeID) error {
	p.connectedVdrs.Remove(nodeID)
	if p.Manager.IsConnected(nodeID) {
		if p.IsPaused(nodeID) {
			// We should never see this case
			return errPausedDisconnect
		}
		return p.Manager.Disconnect(nodeID)
	}
	return nil
}

// IsConnected returns true if the node with the given ID is connected to this manager
// Note: Inner manager may have a different view of the connection status due to pausing
func (p *pausableManager) IsConnected(nodeID ids.NodeID) bool {
	return p.connectedVdrs.Contains(nodeID)
}

// OnValidatorAdded is called when a validator is added.
// If the node is inactive, it will be paused.
func (p *pausableManager) OnValidatorAdded(_ ids.ID, nodeID ids.NodeID, _ uint64, isActive bool) {
	if !isActive {
		err := p.pause(nodeID)
		if err != nil {
			log.Error("failed to handle added validator %s: %s", nodeID, err)
		}
	}
}

// OnValidatorRemoved is called when a validator is removed.
// If the node is already paused, it will be resumed.
func (p *pausableManager) OnValidatorRemoved(_ ids.ID, nodeID ids.NodeID) {
	if p.IsPaused(nodeID) {
		err := p.resume(nodeID)
		if err != nil {
			log.Error("failed to handle validator removed %s: %s", nodeID, err)
		}
	}
}

// OnValidatorStatusUpdated is called when the status of a validator is updated.
// If the node is active, it will be resumed. If the node is inactive, it will be paused.
func (p *pausableManager) OnValidatorStatusUpdated(_ ids.ID, nodeID ids.NodeID, isActive bool) {
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
func (p *pausableManager) IsPaused(nodeID ids.NodeID) bool {
	return p.pausedVdrs.Contains(nodeID)
}

// pause pauses uptime tracking for the node with the given ID
// pause can disconnect the node from the uptime.Manager if it is connected.
func (p *pausableManager) pause(nodeID ids.NodeID) error {
	p.pausedVdrs.Add(nodeID)
	if p.Manager.IsConnected(nodeID) {
		// If the node is connected, then we need to disconnect it from
		// manager
		// This should be fine in case tracking has not started yet since
		// the inner manager should handle disconnects accordingly
		return p.Manager.Disconnect(nodeID)
	}
	return nil
}

// resume resumes uptime tracking for the node with the given ID
// resume can connect the node to the uptime.Manager if it was connected.
func (p *pausableManager) resume(nodeID ids.NodeID) error {
	p.pausedVdrs.Remove(nodeID)
	if p.connectedVdrs.Contains(nodeID) && !p.Manager.IsConnected(nodeID) {
		return p.Manager.Connect(nodeID)
	}
	return nil
}

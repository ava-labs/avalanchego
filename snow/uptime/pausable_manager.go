// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

var ErrPaused = errors.New("node is paused")

type Pausable interface {
	Pause(ids.NodeID) error
	Resume(ids.NodeID) error
	IsPaused(ids.NodeID) bool
}

type PausableManager interface {
	Pausable
	Manager
}

type pausableManager struct {
	Manager
	pausedVdrs    set.Set[ids.NodeID]
	connectedVdrs set.Set[ids.NodeID]
}

func NewPausableManager(manager Manager) PausableManager {
	return &pausableManager{
		pausedVdrs:    make(set.Set[ids.NodeID]),
		connectedVdrs: make(set.Set[ids.NodeID]),
		Manager:       manager,
	}
}

func (p *pausableManager) Pause(nodeID ids.NodeID) error {
	if p.IsPaused(nodeID) {
		return ErrPaused
	}

	p.pausedVdrs.Add(nodeID)
	if !p.Manager.StartedTracking() {
		return nil
	}
	if p.connectedVdrs.Contains(nodeID) {
		// If the node is connected, then we need to disconnect it from
		// manager
		return p.Manager.Disconnect(nodeID)
	}
	return nil
}

func (p *pausableManager) Resume(nodeID ids.NodeID) error {
	p.pausedVdrs.Remove(nodeID)
	if p.connectedVdrs.Contains(nodeID) {
		return p.Manager.Connect(nodeID)
	}
	return nil
}

func (p *pausableManager) IsPaused(nodeID ids.NodeID) bool {
	return p.pausedVdrs.Contains(nodeID)
}

func (p *pausableManager) Connect(nodeID ids.NodeID) error {
	p.connectedVdrs.Add(nodeID)
	if !p.IsPaused(nodeID) {
		return p.Manager.Connect(nodeID)
	}
	return nil
}

func (p *pausableManager) Disconnect(nodeID ids.NodeID) error {
	p.connectedVdrs.Remove(nodeID)
	if !p.IsPaused(nodeID) {
		return p.Manager.Disconnect(nodeID)
	}
	return nil
}

func (p *pausableManager) StartTracking(nodeIDs []ids.NodeID) error {
	var activeNodeIDs []ids.NodeID
	for _, nodeID := range nodeIDs {
		if !p.IsPaused(nodeID) {
			activeNodeIDs = append(activeNodeIDs, nodeID)
		}
	}
	return p.Manager.StartTracking(activeNodeIDs)
}

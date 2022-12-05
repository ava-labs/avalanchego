// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ TestManager = (*manager)(nil)

type Manager interface {
	Tracker
	Calculator
}

type Tracker interface {
	StartTracking(nodeIDs []ids.NodeID, subnetID ids.ID) error
	StopTracking(nodeIDs []ids.NodeID, subnetID ids.ID) error

	Connect(nodeID ids.NodeID, subnetID ids.ID) error
	IsConnected(nodeID ids.NodeID, subnetID ids.ID) bool
	Disconnect(nodeID ids.NodeID) error
}

type Calculator interface {
	CalculateUptime(nodeID ids.NodeID, subnetID ids.ID) (time.Duration, time.Time, error)
	CalculateUptimePercent(nodeID ids.NodeID, subnetID ids.ID) (float64, error)
	// CalculateUptimePercentFrom expects [startTime] to be truncated (floored) to the nearest second
	CalculateUptimePercentFrom(nodeID ids.NodeID, subnetID ids.ID, startTime time.Time) (float64, error)
}

type TestManager interface {
	Manager
	SetTime(time.Time)
}

type manager struct {
	// Used to get time. Useful for faking time during tests.
	clock mockable.Clock

	state          State
	connections    map[ids.NodeID]map[ids.ID]time.Time // nodeID -> subnetID -> time
	trackedSubnets set.Set[ids.ID]
}

func NewManager(state State) Manager {
	return &manager{
		state:       state,
		connections: make(map[ids.NodeID]map[ids.ID]time.Time),
	}
}

func (m *manager) StartTracking(nodeIDs []ids.NodeID, subnetID ids.ID) error {
	now := m.clock.UnixTime()
	for _, nodeID := range nodeIDs {
		upDuration, lastUpdated, err := m.state.GetUptime(nodeID, subnetID)
		if err != nil {
			return err
		}

		// If we are in a weird reality where time has moved backwards, then we
		// shouldn't modify the validator's uptime.
		if now.Before(lastUpdated) {
			continue
		}

		durationOffline := now.Sub(lastUpdated)
		newUpDuration := upDuration + durationOffline
		if err := m.state.SetUptime(nodeID, subnetID, newUpDuration, now); err != nil {
			return err
		}
	}
	m.trackedSubnets.Add(subnetID)
	return nil
}

func (m *manager) StopTracking(nodeIDs []ids.NodeID, subnetID ids.ID) error {
	now := m.clock.UnixTime()
	for _, nodeID := range nodeIDs {
		connectedSubnets := m.connections[nodeID]
		// If the node is already connected to this subnet, then we can just
		// update the uptime in the state and remove the connection
		if _, isConnected := connectedSubnets[subnetID]; isConnected {
			if err := m.updateSubnetUptime(nodeID, subnetID); err != nil {
				delete(connectedSubnets, subnetID)
				return err
			}
			delete(connectedSubnets, subnetID)
			continue
		}

		// if the node is not connected to this subnet, then we need to update
		// the uptime in the state from the last time the node was connected to
		// this subnet to now.
		upDuration, lastUpdated, err := m.state.GetUptime(nodeID, subnetID)
		if err != nil {
			return err
		}

		// If we are in a weird reality where time has moved backwards, then we
		// shouldn't modify the validator's uptime.
		if now.Before(lastUpdated) {
			continue
		}

		if err := m.state.SetUptime(nodeID, subnetID, upDuration, now); err != nil {
			return err
		}
	}
	return nil
}

func (m *manager) Connect(nodeID ids.NodeID, subnetID ids.ID) error {
	subnetConnections, ok := m.connections[nodeID]
	if !ok {
		subnetConnections = make(map[ids.ID]time.Time)
		m.connections[nodeID] = subnetConnections
	}
	subnetConnections[subnetID] = m.clock.UnixTime()
	return nil
}

func (m *manager) IsConnected(nodeID ids.NodeID, subnetID ids.ID) bool {
	_, connected := m.connections[nodeID][subnetID]
	return connected
}

func (m *manager) Disconnect(nodeID ids.NodeID) error {
	// Update every subnet that this node was connected to
	for subnetID := range m.connections[nodeID] {
		if err := m.updateSubnetUptime(nodeID, subnetID); err != nil {
			return err
		}
	}
	delete(m.connections, nodeID)
	return nil
}

func (m *manager) CalculateUptime(nodeID ids.NodeID, subnetID ids.ID) (time.Duration, time.Time, error) {
	upDuration, lastUpdated, err := m.state.GetUptime(nodeID, subnetID)
	if err != nil {
		return 0, time.Time{}, err
	}

	now := m.clock.UnixTime()
	// If we are in a weird reality where time has gone backwards, make sure
	// that we don't double count or delete any uptime.
	if now.Before(lastUpdated) {
		return upDuration, lastUpdated, nil
	}

	if !m.trackedSubnets.Contains(subnetID) {
		durationOffline := now.Sub(lastUpdated)
		newUpDuration := upDuration + durationOffline
		return newUpDuration, now, nil
	}

	timeConnected, isConnected := m.connections[nodeID][subnetID]
	if !isConnected {
		return upDuration, now, nil
	}

	// The time the peer connected needs to be adjusted to ensure no time period
	// is double counted.
	if timeConnected.Before(lastUpdated) {
		timeConnected = lastUpdated
	}

	// If we are in a weird reality where time has gone backwards, make sure
	// that we don't double count or delete any uptime.
	if now.Before(timeConnected) {
		return upDuration, now, nil
	}

	// Increase the uptimes by the amount of time this node has been running
	// since the last time it's uptime was written to disk.
	durationConnected := now.Sub(timeConnected)
	newUpDuration := upDuration + durationConnected
	return newUpDuration, now, nil
}

func (m *manager) CalculateUptimePercent(nodeID ids.NodeID, subnetID ids.ID) (float64, error) {
	startTime, err := m.state.GetStartTime(nodeID, subnetID)
	if err != nil {
		return 0, err
	}
	return m.CalculateUptimePercentFrom(nodeID, subnetID, startTime)
}

func (m *manager) CalculateUptimePercentFrom(nodeID ids.NodeID, subnetID ids.ID, startTime time.Time) (float64, error) {
	upDuration, now, err := m.CalculateUptime(nodeID, subnetID)
	if err != nil {
		return 0, err
	}
	bestPossibleUpDuration := now.Sub(startTime)
	if bestPossibleUpDuration == 0 {
		return 1, nil
	}
	uptime := float64(upDuration) / float64(bestPossibleUpDuration)
	return uptime, nil
}

func (m *manager) SetTime(newTime time.Time) {
	m.clock.Set(newTime)
}

// updateSubnetUptime updates the subnet uptime of the node on the state by the amount
// of time that the node has been connected to the subnet.
func (m *manager) updateSubnetUptime(nodeID ids.NodeID, subnetID ids.ID) error {
	// we're not tracking this subnet, skip updating it.
	if !m.trackedSubnets.Contains(subnetID) {
		return nil
	}

	newDuration, newLastUpdated, err := m.CalculateUptime(nodeID, subnetID)
	if err == database.ErrNotFound {
		// If a non-validator disconnects, we don't care
		return nil
	}
	if err != nil {
		return err
	}

	return m.state.SetUptime(nodeID, subnetID, newDuration, newLastUpdated)
}

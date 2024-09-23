// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ Manager = (*manager)(nil)

type Manager interface {
	Tracker
	Calculator
}

type Tracker interface {
	StartTracking(nodeIDs []ids.NodeID) error
	StopTracking(nodeIDs []ids.NodeID) error
	StartedTracking() bool

	Connect(nodeID ids.NodeID) error
	IsConnected(nodeID ids.NodeID) bool
	Disconnect(nodeID ids.NodeID) error
}

type Calculator interface {
	CalculateUptime(nodeID ids.NodeID) (time.Duration, time.Time, error)
	CalculateUptimePercent(nodeID ids.NodeID) (float64, error)
	// CalculateUptimePercentFrom expects [startTime] to be truncated (floored) to the nearest second
	CalculateUptimePercentFrom(nodeID ids.NodeID, startTime time.Time) (float64, error)
}

type manager struct {
	// Used to get time. Useful for faking time during tests.
	clock *mockable.Clock

	state       State
	connections map[ids.NodeID]time.Time // nodeID  -> connected at
	// Whether we have started tracking the uptime of the nodes
	// This is used to avoid setting the uptime before we have started tracking
	startedTracking bool
}

func NewManager(state State, clk *mockable.Clock) Manager {
	return &manager{
		clock:       clk,
		state:       state,
		connections: make(map[ids.NodeID]time.Time),
	}
}

func (m *manager) StartTracking(nodeIDs []ids.NodeID) error {
	if m.startedTracking {
		return nil
	}
	now := m.clock.UnixTime()
	for _, nodeID := range nodeIDs {
		upDuration, lastUpdated, err := m.CalculateUptime(nodeID)
		if err != nil {
			return err
		}

		// If we are in a weird reality where time has moved backwards, then we
		// shouldn't modify the validator's uptime.
		if now.Before(lastUpdated) {
			continue
		}

		if err := m.state.SetUptime(nodeID, upDuration, lastUpdated); err != nil {
			return err
		}
	}
	m.startedTracking = true
	return nil
}

func (m *manager) StopTracking(nodeIDs []ids.NodeID) error {
	if !m.startedTracking {
		return nil
	}
	defer func() {
		m.startedTracking = false
	}()
	now := m.clock.UnixTime()
	for _, nodeID := range nodeIDs {
		// If the node is already connected, then we can just
		// update the uptime in the state and remove the connection
		if m.IsConnected(nodeID) {
			if err := m.Disconnect(nodeID); err != nil {
				return err
			}
			continue
		}

		// if the node is not connected, then we need to update
		// the uptime in the state from the last time the node was connected to current time.
		upDuration, lastUpdated, err := m.state.GetUptime(nodeID)
		if err != nil {
			return err
		}

		// If we are in a weird reality where time has moved backwards, then we
		// shouldn't modify the validator's uptime.
		if now.Before(lastUpdated) {
			continue
		}

		if err := m.state.SetUptime(nodeID, upDuration, now); err != nil {
			return err
		}
	}
	return nil
}

func (m *manager) StartedTracking() bool {
	return m.startedTracking
}

func (m *manager) Connect(nodeID ids.NodeID) error {
	m.connections[nodeID] = m.clock.UnixTime()
	return nil
}

func (m *manager) IsConnected(nodeID ids.NodeID) bool {
	_, connected := m.connections[nodeID]
	return connected
}

func (m *manager) Disconnect(nodeID ids.NodeID) error {
	defer delete(m.connections, nodeID)
	if err := m.updateUptime(nodeID); err != nil {
		return err
	}

	return nil
}

func (m *manager) CalculateUptime(nodeID ids.NodeID) (time.Duration, time.Time, error) {
	upDuration, lastUpdated, err := m.state.GetUptime(nodeID)
	if err != nil {
		return 0, time.Time{}, err
	}

	now := m.clock.UnixTime()
	// If we are in a weird reality where time has gone backwards, make sure
	// that we don't double count or delete any uptime.
	if now.Before(lastUpdated) {
		return upDuration, lastUpdated, nil
	}

	if !m.startedTracking {
		durationOffline := now.Sub(lastUpdated)
		newUpDuration := upDuration + durationOffline
		return newUpDuration, now, nil
	}

	timeConnected, isConnected := m.connections[nodeID]
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

func (m *manager) CalculateUptimePercent(nodeID ids.NodeID) (float64, error) {
	startTime, err := m.state.GetStartTime(nodeID)
	if err != nil {
		return 0, err
	}
	return m.CalculateUptimePercentFrom(nodeID, startTime)
}

func (m *manager) CalculateUptimePercentFrom(nodeID ids.NodeID, startTime time.Time) (float64, error) {
	upDuration, now, err := m.CalculateUptime(nodeID)
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

// updateUptime updates the uptime of the node on the state by the amount
// of time that the node has been connected.
func (m *manager) updateUptime(nodeID ids.NodeID) error {
	if !m.startedTracking {
		return nil
	}
	newDuration, newLastUpdated, err := m.CalculateUptime(nodeID)
	if err == database.ErrNotFound {
		// If a non-validator disconnects, we don't care
		return nil
	}
	if err != nil {
		return err
	}

	return m.state.SetUptime(nodeID, newDuration, newLastUpdated)
}

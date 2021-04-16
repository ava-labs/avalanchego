// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer"
)

type State interface {
	GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error)
	SetUptime(nodeID ids.ShortID, upDuration time.Duration, lastUpdated time.Time) error
}

type Manager interface {
	// Should only be called once
	StartTracking(nodeIDs []ids.ShortID) error
	Shutdown() error

	Connect(nodeID ids.ShortID) error
	Disconnect(nodeID ids.ShortID) error

	CalculateUptime(nodeID ids.ShortID) (time.Duration, time.Time, error)
	CalculateUptimePercent(nodeID ids.ShortID, startTime time.Time) (float64, error)
}

type manager struct {
	// Used to get time. Useful for faking time during tests.
	clock timer.Clock

	state       State
	connections map[ids.ShortID]time.Time

	startedTime time.Time
}

func NewManager(state State) Manager {
	return &manager{
		state:       state,
		connections: make(map[ids.ShortID]time.Time),
	}
}

func (m *manager) StartTracking(nodeIDs []ids.ShortID) error {
	m.startedTime = m.clock.Time()

	for _, nodeID := range nodeIDs {
		upDuration, lastUpdated, err := m.state.GetUptime(nodeID)
		if err != nil {
			return err
		}

		durationOffline := m.startedTime.Sub(lastUpdated)
		newUpDuration := upDuration + durationOffline
		if err := m.state.SetUptime(nodeID, newUpDuration, m.startedTime); err != nil {
			return err
		}
	}
	return nil
}

func (m *manager) Shutdown() error {
	for nodeID := range m.connections {
		if err := m.Disconnect(nodeID); err != nil {
			return err
		}
	}
	return nil
}

func (m *manager) Connect(nodeID ids.ShortID) error {
	m.connections[nodeID] = m.clock.Time()
	return nil
}

func (m *manager) Disconnect(nodeID ids.ShortID) error {
	newDuration, newLastUpdated, err := m.CalculateUptime(nodeID)
	if err == database.ErrNotFound {
		// If a non-validator disconnects, we don't care
		return nil
	}
	if err != nil {
		return err
	}
	delete(m.connections, nodeID)
	return m.state.SetUptime(nodeID, newDuration, newLastUpdated)
}

func (m *manager) CalculateUptime(nodeID ids.ShortID) (time.Duration, time.Time, error) {
	upDuration, lastUpdated, err := m.state.GetUptime(nodeID)
	if err != nil {
		return 0, time.Time{}, err
	}

	currentLocalTime := m.clock.Time()
	if timeConnected, isConnected := m.connections[nodeID]; isConnected {
		// The time the peer connected should be set to:
		// max(realTimeConnected, startedTime, lastUpdated)
		if timeConnected.Before(m.startedTime) {
			timeConnected = m.startedTime
		}
		if timeConnected.Before(lastUpdated) {
			timeConnected = lastUpdated
		}

		// Increase the uptimes by the amount of time this node has been running
		// since the last time it's uptime was written to disk.
		if durationConnected := currentLocalTime.Sub(timeConnected); durationConnected > 0 {
			upDuration += durationConnected
		}
	}
	return upDuration, currentLocalTime, nil
}

func (m *manager) CalculateUptimePercent(nodeID ids.ShortID, startTime time.Time) (float64, error) {
	upDuration, currentLocalTime, err := m.CalculateUptime(nodeID)
	if err != nil {
		return 0, err
	}
	bestPossibleUpDuration := currentLocalTime.Sub(startTime)
	uptime := float64(upDuration) / float64(bestPossibleUpDuration)
	return uptime, nil
}

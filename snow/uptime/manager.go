// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ Manager = (*manager)(nil)

var (
	errAlreadyStartedTracking = errors.New("already started tracking")
	errNotStartedTracking     = errors.New("not started tracking")
)

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
		return errAlreadyStartedTracking
	}

	for _, nodeID := range nodeIDs {
		if err := m.updateUptime(nodeID); err != nil {
			return err
		}
	}

	m.startedTracking = true
	return nil
}

func (m *manager) StopTracking(nodeIDs []ids.NodeID) error {
	if !m.startedTracking {
		return errNotStartedTracking
	}

	for _, nodeID := range nodeIDs {
		if err := m.updateUptime(nodeID); err != nil {
			return err
		}
	}

	m.startedTracking = false
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

	if !m.startedTracking {
		return nil
	}

	return m.updateUptime(nodeID)
}

// The `CalculateUptime` method calculates a node's uptime based on its connection status,
// connected time, and the current time. It first retrieves the node's current uptime and
// last update time from the state, returning an error if retrieval fails. If tracking hasn’t
// started, it assumes the node has been online since the last update, adding this duration
// to its uptime. If the node is not connected and tracking is `active`, uptime remains
// unchanged and returned. For connected nodes, the method ensures the connection time does
// not predate the last update to avoid double counting. Finally, it adds the duration since
// the last connection time to the node's uptime and returns the updated values.
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

	// If we haven't started tracking, then we assume that the node has been
	// online since their last update.
	if !m.startedTracking {
		durationOffline := now.Sub(lastUpdated)
		newUpDuration := upDuration + durationOffline
		return newUpDuration, now, nil
	}

	// If we are tracking and they aren't connected, they have been offline
	// since their last update.
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

// updateUptime updates the uptime of the node on the state by the amount of
// time that the node has been connected.
func (m *manager) updateUptime(nodeID ids.NodeID) error {
	newDuration, newLastUpdated, err := m.CalculateUptime(nodeID)
	if err == database.ErrNotFound {
		// We don't track the uptimes of non-validators.
		return nil
	}
	if err != nil {
		return err
	}

	return m.state.SetUptime(nodeID, newDuration, newLastUpdated)
}

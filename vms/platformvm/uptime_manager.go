// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer"
)

type UptimeState interface {
	GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error)
	SetUptime(nodeID ids.ShortID, upDuration time.Duration, lastUpdated time.Time) error
}

type UptimeManager interface {
	// Should only be called once
	StartTracking(nodeIDs []ids.ShortID) error
	Shutdown() error

	Connect(nodeID ids.ShortID) error
	Disconnect(nodeID ids.ShortID) error

	CalculateUptime(nodeID ids.ShortID) (time.Duration, time.Time, error)
	CalculateUptimePercent(nodeID ids.ShortID, startTime time.Time) (float64, error)
}

type uptimeManager struct {
	// Used to get time. Useful for faking time during tests.
	clock timer.Clock

	state       UptimeState
	connections map[ids.ShortID]time.Time

	startedTime time.Time
}

func NewUptimeManager(state UptimeState) UptimeManager {
	return &uptimeManager{
		state:       state,
		connections: make(map[ids.ShortID]time.Time),
	}
}

func (up *uptimeManager) StartTracking(nodeIDs []ids.ShortID) error {
	up.startedTime = up.clock.Time()

	for _, nodeID := range nodeIDs {
		upDuration, lastUpdated, err := up.state.GetUptime(nodeID)
		if err != nil {
			return err
		}

		durationOffline := up.startedTime.Sub(lastUpdated)
		newUpDuration := upDuration + durationOffline
		if err := up.state.SetUptime(nodeID, newUpDuration, up.startedTime); err != nil {
			return err
		}
	}
	return nil
}

func (up *uptimeManager) Shutdown() error {
	for nodeID := range up.connections {
		if err := up.Disconnect(nodeID); err != nil {
			return err
		}
	}
	return nil
}

func (up *uptimeManager) Connect(nodeID ids.ShortID) error {
	up.connections[nodeID] = up.clock.Time()
	return nil
}

func (up *uptimeManager) Disconnect(nodeID ids.ShortID) error {
	newDuration, newLastUpdated, err := up.CalculateUptime(nodeID)
	if err == database.ErrNotFound {
		// If a non-validator disconnects, we don't care
		return nil
	}
	if err != nil {
		return err
	}
	delete(up.connections, nodeID)
	return up.state.SetUptime(nodeID, newDuration, newLastUpdated)
}

func (up *uptimeManager) CalculateUptime(nodeID ids.ShortID) (time.Duration, time.Time, error) {
	upDuration, lastUpdated, err := up.state.GetUptime(nodeID)
	if err != nil {
		return 0, time.Time{}, err
	}

	currentLocalTime := up.clock.Time()
	if timeConnected, isConnected := up.connections[nodeID]; isConnected {
		// The time the peer connected should be set to:
		// max(realTimeConnected, startedTime, lastUpdated)
		if timeConnected.Before(up.startedTime) {
			timeConnected = up.startedTime
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

func (up *uptimeManager) CalculateUptimePercent(nodeID ids.ShortID, startTime time.Time) (float64, error) {
	upDuration, currentLocalTime, err := up.CalculateUptime(nodeID)
	if err != nil {
		return 0, err
	}
	bestPossibleUpDuration := currentLocalTime.Sub(startTime)
	uptime := float64(upDuration) / float64(bestPossibleUpDuration)
	return uptime, nil
}

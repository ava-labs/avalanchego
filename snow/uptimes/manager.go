// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimes

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

type Manager interface {
	// Should only be called once
	StartTracking(nodeIDs []ids.ShortID) error

	// Should only be called once
	Shutdown(nodeIDs []ids.ShortID) error

	Connect(nodeID ids.ShortID) error
	IsConnected(nodeID ids.ShortID) bool
	Disconnect(nodeID ids.ShortID) error

	CalculateUptime(nodeID ids.ShortID) (time.Duration, time.Time, error)
	CalculateUptimePercent(nodeID ids.ShortID, startTime time.Time) (float64, error)
	CalculateCurrentUptimePercent(nodeID ids.ShortID) (float64, error)

	SetState(state UptimeState) error
}

var _ TestUptimeManager = &uptimeManager{}

type UptimeState interface {
	GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error)
	SetUptime(nodeID ids.ShortID, upDuration time.Duration, lastUpdated time.Time) error
	GetStartTime(nodeID ids.ShortID) (startTime time.Time, err error)
}

type TestUptimeManager interface {
	Manager
	SetTime(time.Time)
}

type uptimeManager struct {
	// Used to get time. Useful for faking time during tests.
	clock mockable.Clock

	state           UptimeState
	connections     map[ids.ShortID]time.Time
	startedTracking bool
}

func NewUptimeManager(state UptimeState) Manager {
	return &uptimeManager{
		state:       state,
		connections: make(map[ids.ShortID]time.Time),
	}
}

func (u *uptimeManager) SetState(state UptimeState) error {
	u.state = state
	return nil
}

func (u *uptimeManager) StartTracking(nodeIDs []ids.ShortID) error {
	currentLocalTime := u.clock.Time()
	for _, nodeID := range nodeIDs {
		upDuration, lastUpdated, err := u.state.GetUptime(nodeID)
		if err != nil {
			return err
		}

		// If we are in a weird reality where time has moved backwards, then we
		// shouldn't modify the validator's uptime.
		if currentLocalTime.Before(lastUpdated) {
			continue
		}

		durationOffline := currentLocalTime.Sub(lastUpdated)
		newUpDuration := upDuration + durationOffline
		if err := u.state.SetUptime(nodeID, newUpDuration, currentLocalTime); err != nil {
			return err
		}
	}
	u.startedTracking = true
	return nil
}

func (u *uptimeManager) Shutdown(nodeIDs []ids.ShortID) error {
	currentLocalTime := u.clock.Time()
	for _, nodeID := range nodeIDs {
		if _, connected := u.connections[nodeID]; connected {
			if err := u.Disconnect(nodeID); err != nil {
				return err
			}
			continue
		}

		upDuration, lastUpdated, err := u.state.GetUptime(nodeID)
		if err != nil {
			return err
		}

		// If we are in a weird reality where time has moved backwards, then we
		// shouldn't modify the validator's uptime.
		if currentLocalTime.Before(lastUpdated) {
			continue
		}

		if err := u.state.SetUptime(nodeID, upDuration, currentLocalTime); err != nil {
			return err
		}
	}
	return nil
}

func (u *uptimeManager) Connect(nodeID ids.ShortID) error {
	u.connections[nodeID] = u.clock.Time()
	return nil
}

func (u *uptimeManager) IsConnected(nodeID ids.ShortID) bool {
	_, connected := u.connections[nodeID]
	return connected
}

func (u *uptimeManager) Disconnect(nodeID ids.ShortID) error {
	if !u.startedTracking {
		delete(u.connections, nodeID)
		return nil
	}

	newDuration, newLastUpdated, err := u.CalculateUptime(nodeID)
	delete(u.connections, nodeID)
	if err == database.ErrNotFound {
		// If a non-validator disconnects, we don't care
		return nil
	}
	if err != nil {
		return err
	}
	return u.state.SetUptime(nodeID, newDuration, newLastUpdated)
}

func (u *uptimeManager) CalculateUptime(nodeID ids.ShortID) (time.Duration, time.Time, error) {
	upDuration, lastUpdated, err := u.state.GetUptime(nodeID)
	if err != nil {
		return 0, time.Time{}, err
	}

	currentLocalTime := u.clock.Time()
	// If we are in a weird reality where time has gone backwards, make sure
	// that we don't double count or delete any uptime.
	if currentLocalTime.Before(lastUpdated) {
		return upDuration, lastUpdated, nil
	}

	timeConnected, isConnected := u.connections[nodeID]
	if !isConnected {
		return upDuration, currentLocalTime, nil
	}

	// The time the peer connected needs to be adjusted to ensure no time period
	// is double counted.
	if timeConnected.Before(lastUpdated) {
		timeConnected = lastUpdated
	}

	// If we are in a weird reality where time has gone backwards, make sure
	// that we don't double count or delete any uptime.
	if currentLocalTime.Before(timeConnected) {
		return upDuration, currentLocalTime, nil
	}

	// Increase the uptimes by the amount of time this node has been running
	// since the last time it's uptime was written to disk.
	durationConnected := currentLocalTime.Sub(timeConnected)
	newUpDuration := upDuration + durationConnected
	return newUpDuration, currentLocalTime, nil
}

func (u *uptimeManager) CalculateUptimePercent(nodeID ids.ShortID, startTime time.Time) (float64, error) {
	upDuration, currentLocalTime, err := u.CalculateUptime(nodeID)
	if err != nil {
		return 0, err
	}
	bestPossibleUpDuration := currentLocalTime.Sub(startTime)
	if bestPossibleUpDuration == 0 {
		return 1, nil
	}
	uptime := float64(upDuration) / float64(bestPossibleUpDuration)
	return uptime, nil
}

func (u *uptimeManager) CalculateCurrentUptimePercent(nodeID ids.ShortID) (float64, error) {
	startTime, err := u.state.GetStartTime(nodeID)
	if err != nil {
		return 0, err
	}
	return u.CalculateUptimePercent(nodeID, startTime)
}

func (u *uptimeManager) SetTime(newTime time.Time) {
	u.clock.Set(newTime)
}

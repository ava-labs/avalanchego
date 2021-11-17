// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

func TestStartTracking(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)

	currentTime := startTime.Add(time.Second)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Second, duration)
	assert.Equal(currentTime, lastUpdated)
}

func TestStartTrackingDBError(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	startTime := time.Now()

	s := NewTestState()
	s.dbWriteError = errors.New("err")
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)

	currentTime := startTime.Add(time.Second)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.ShortID{nodeID0})
	assert.Error(err)
}

func TestStartTrackingNonValidator(t *testing.T) {
	assert := assert.New(t)

	s := NewTestState()
	up := NewManager(s).(*manager)

	nodeID0 := ids.GenerateTestShortID()
	err := up.StartTracking([]ids.ShortID{nodeID0})
	assert.Error(err)
}

func TestStartTrackingInThePast(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)

	currentTime := startTime.Add(-time.Second)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Duration(0), duration)
	assert.Equal(startTime, lastUpdated)
}

func TestShutdownDecreasesUptime(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	currentTime = startTime.Add(time.Second)
	up.clock.Set(currentTime)

	err = up.Shutdown([]ids.ShortID{nodeID0})
	assert.NoError(err)

	up = NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err = up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Duration(0), duration)
	assert.Equal(currentTime, lastUpdated)
}

func TestShutdownIncreasesUptime(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	err = up.Connect(nodeID0)
	assert.NoError(err)

	currentTime = startTime.Add(time.Second)
	up.clock.Set(currentTime)

	err = up.Shutdown([]ids.ShortID{nodeID0})
	assert.NoError(err)

	up = NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err = up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Second, duration)
	assert.Equal(currentTime, lastUpdated)
}

func TestShutdownDisconnectedNonValidator(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()

	s := NewTestState()
	up := NewManager(s).(*manager)

	err := up.StartTracking(nil)
	assert.NoError(err)

	err = up.Shutdown([]ids.ShortID{nodeID0})
	assert.Error(err)
}

func TestShutdownConnectedDBError(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)
	up := NewManager(s).(*manager)

	err := up.StartTracking(nil)
	assert.NoError(err)

	err = up.Connect(nodeID0)
	assert.NoError(err)

	s.dbReadError = errors.New("err")
	err = up.Shutdown([]ids.ShortID{nodeID0})
	assert.Error(err)
}

func TestShutdownNonConnectedPast(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)
	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	currentTime = currentTime.Add(-time.Second)
	up.clock.Set(currentTime)

	err = up.Shutdown([]ids.ShortID{nodeID0})
	assert.NoError(err)

	duration, lastUpdated, err := s.GetUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Duration(0), duration)
	assert.Equal(startTime, lastUpdated)
}

func TestShutdownNonConnectedDBError(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)
	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	s.dbWriteError = errors.New("err")
	err = up.Shutdown([]ids.ShortID{nodeID0})
	assert.Error(err)
}

func TestConnectAndDisconnect(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	connected := up.IsConnected(nodeID0)
	assert.False(connected)

	err := up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	connected = up.IsConnected(nodeID0)
	assert.False(connected)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Duration(0), duration)
	assert.Equal(currentTime, lastUpdated)

	err = up.Connect(nodeID0)
	assert.NoError(err)

	connected = up.IsConnected(nodeID0)
	assert.True(connected)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Second, duration)
	assert.Equal(currentTime, lastUpdated)

	err = up.Disconnect(nodeID0)
	assert.NoError(err)

	connected = up.IsConnected(nodeID0)
	assert.False(connected)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Second, duration)
	assert.Equal(currentTime, lastUpdated)
}

func TestConnectAndDisconnectBeforeTracking(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	err := up.Connect(nodeID0)
	assert.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	err = up.Disconnect(nodeID0)
	assert.NoError(err)

	err = up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(2*time.Second, duration)
	assert.Equal(currentTime, lastUpdated)
}

func TestUnrelatedNodeDisconnect(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	nodeID1 := ids.GenerateTestShortID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Duration(0), duration)
	assert.Equal(currentTime, lastUpdated)

	err = up.Connect(nodeID0)
	assert.NoError(err)

	err = up.Connect(nodeID1)
	assert.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Second, duration)
	assert.Equal(currentTime, lastUpdated)

	err = up.Disconnect(nodeID1)
	assert.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(2*time.Second, duration)
	assert.Equal(currentTime, lastUpdated)
}

func TestCalculateUptimeWhenNeverConnected(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)

	currentTime := startTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Duration(0), duration)
	assert.Equal(currentTime, lastUpdated)

	uptime, err := up.CalculateUptimePercentFrom(nodeID0, startTime)
	assert.NoError(err)
	assert.Equal(0., uptime)
}

func TestCalculateUptimeWhenConnectedBeforeTracking(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.Connect(nodeID0)
	assert.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	err = up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(2*time.Second, duration)
	assert.Equal(currentTime, lastUpdated)
}

func TestCalculateUptimeWhenConnectedInFuture(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.ShortID{nodeID0})
	assert.NoError(err)

	currentTime = currentTime.Add(2 * time.Second)
	up.clock.Set(currentTime)

	err = up.Connect(nodeID0)
	assert.NoError(err)

	currentTime = currentTime.Add(-time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	assert.NoError(err)
	assert.Equal(time.Duration(0), duration)
	assert.Equal(currentTime, lastUpdated)
}

func TestCalculateUptimeNonValidator(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	startTime := time.Now()

	s := NewTestState()

	up := NewManager(s).(*manager)

	_, err := up.CalculateUptimePercentFrom(nodeID0, startTime)
	assert.Error(err)
}

func TestCalculateUptimePercentageDivBy0(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	uptime, err := up.CalculateUptimePercentFrom(nodeID0, startTime)
	assert.NoError(err)
	assert.Equal(float64(1), uptime)
}

func TestCalculateUptimePercentage(t *testing.T) {
	assert := assert.New(t)

	nodeID0 := ids.GenerateTestShortID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	uptime, err := up.CalculateUptimePercentFrom(nodeID0, startTime)
	assert.NoError(err)
	assert.Equal(float64(0), uptime)
}

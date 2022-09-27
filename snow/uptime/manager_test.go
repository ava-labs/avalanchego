// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestStartTracking(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)

	currentTime := startTime.Add(time.Second)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Second, duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)
}

func TestStartTrackingDBError(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()
	s.dbWriteError = errors.New("err")
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)

	currentTime := startTime.Add(time.Second)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.Error(err)
}

func TestStartTrackingNonValidator(t *testing.T) {
	require := require.New(t)

	s := NewTestState()
	up := NewManager(s).(*manager)

	nodeID0 := ids.GenerateTestNodeID()
	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.Error(err)
}

func TestStartTrackingInThePast(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)

	currentTime := startTime.Add(-time.Second)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(startTime.Truncate(time.Second), lastUpdated)
}

func TestShutdownDecreasesUptime(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	currentTime = startTime.Add(time.Second)
	up.clock.Set(currentTime)

	err = up.Shutdown([]ids.NodeID{nodeID0})
	require.NoError(err)

	up = NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err = up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)
}

func TestShutdownIncreasesUptime(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	err = up.Connect(nodeID0)
	require.NoError(err)

	currentTime = startTime.Add(time.Second)
	up.clock.Set(currentTime)

	err = up.Shutdown([]ids.NodeID{nodeID0})
	require.NoError(err)

	up = NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err = up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Second, duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)
}

func TestShutdownDisconnectedNonValidator(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()

	s := NewTestState()
	up := NewManager(s).(*manager)

	err := up.StartTracking(nil)
	require.NoError(err)

	err = up.Shutdown([]ids.NodeID{nodeID0})
	require.Error(err)
}

func TestShutdownConnectedDBError(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)
	up := NewManager(s).(*manager)

	err := up.StartTracking(nil)
	require.NoError(err)

	err = up.Connect(nodeID0)
	require.NoError(err)

	s.dbReadError = errors.New("err")
	err = up.Shutdown([]ids.NodeID{nodeID0})
	require.Error(err)
}

func TestShutdownNonConnectedPast(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)
	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	currentTime = currentTime.Add(-time.Second)
	up.clock.Set(currentTime)

	err = up.Shutdown([]ids.NodeID{nodeID0})
	require.NoError(err)

	duration, lastUpdated, err := s.GetUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(startTime.Truncate(time.Second), lastUpdated)
}

func TestShutdownNonConnectedDBError(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)
	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	s.dbWriteError = errors.New("err")
	err = up.Shutdown([]ids.NodeID{nodeID0})
	require.Error(err)
}

func TestConnectAndDisconnect(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	connected := up.IsConnected(nodeID0)
	require.False(connected)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	connected = up.IsConnected(nodeID0)
	require.False(connected)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)

	err = up.Connect(nodeID0)
	require.NoError(err)

	connected = up.IsConnected(nodeID0)
	require.True(connected)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Second, duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)

	err = up.Disconnect(nodeID0)
	require.NoError(err)

	connected = up.IsConnected(nodeID0)
	require.False(connected)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Second, duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)
}

func TestConnectAndDisconnectBeforeTracking(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	err := up.Connect(nodeID0)
	require.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	err = up.Disconnect(nodeID0)
	require.NoError(err)

	err = up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(2*time.Second, duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)
}

func TestUnrelatedNodeDisconnect(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	nodeID1 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)

	err = up.Connect(nodeID0)
	require.NoError(err)

	err = up.Connect(nodeID1)
	require.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Second, duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)

	err = up.Disconnect(nodeID1)
	require.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(2*time.Second, duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)
}

func TestCalculateUptimeWhenNeverConnected(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)

	currentTime := startTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)

	uptime, err := up.CalculateUptimePercentFrom(nodeID0, startTime)
	require.NoError(err)
	require.Equal(0., uptime)
}

func TestCalculateUptimeWhenConnectedBeforeTracking(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.Connect(nodeID0)
	require.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	err = up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(2*time.Second, duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)
}

func TestCalculateUptimeWhenConnectedInFuture(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	currentTime = currentTime.Add(2 * time.Second)
	up.clock.Set(currentTime)

	err = up.Connect(nodeID0)
	require.NoError(err)

	currentTime = currentTime.Add(-time.Second)
	up.clock.Set(currentTime)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(up.clock.UnixTime(), lastUpdated)
}

func TestCalculateUptimeNonValidator(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()

	up := NewManager(s).(*manager)

	_, err := up.CalculateUptimePercentFrom(nodeID0, startTime)
	require.Error(err)
}

func TestCalculateUptimePercentageDivBy0(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	uptime, err := up.CalculateUptimePercentFrom(nodeID0, startTime.Truncate(time.Second))
	require.NoError(err)
	require.Equal(float64(1), uptime)
}

func TestCalculateUptimePercentage(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)

	currentTime = currentTime.Add(time.Second)
	up.clock.Set(currentTime)

	uptime, err := up.CalculateUptimePercentFrom(nodeID0, startTime)
	require.NoError(err)
	require.Equal(float64(0), uptime)
}

func TestShutdownUnixTimeRegression(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	up := NewManager(s).(*manager)
	up.clock.Set(currentTime)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	err = up.Connect(nodeID0)
	require.NoError(err)

	currentTime = startTime.Add(time.Second)
	up.clock.Set(currentTime)

	err = up.Shutdown([]ids.NodeID{nodeID0})
	require.NoError(err)

	currentTime = startTime.Add(time.Second)
	up.clock.Set(currentTime)

	up = NewManager(s).(*manager)

	currentTime = startTime.Add(time.Second)
	up.clock.Set(currentTime)

	err = up.StartTracking([]ids.NodeID{nodeID0})
	require.NoError(err)

	err = up.Connect(nodeID0)
	require.NoError(err)

	currentTime = startTime.Add(time.Second)
	up.clock.Set(currentTime)

	perc, err := up.CalculateUptimePercent(nodeID0)
	require.NoError(err)
	require.GreaterOrEqual(float64(1), perc)
}

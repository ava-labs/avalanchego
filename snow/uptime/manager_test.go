// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var errTest = errors.New("non-nil error")

func TestStartTracking(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)

	currentTime := startTime.Add(time.Second)
	clk.Set(currentTime)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Second, duration)
	require.Equal(clk.UnixTime(), lastUpdated)
}

func TestStartTrackingDBError(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()
	s.dbWriteError = errTest
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)

	currentTime := startTime.Add(time.Second)
	clk.Set(currentTime)

	err := up.StartTracking([]ids.NodeID{nodeID0})
	require.ErrorIs(err, errTest)
}

func TestStartTrackingInThePast(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)

	currentTime := startTime.Add(-time.Second)
	clk.Set(currentTime)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(startTime.Truncate(time.Second), lastUpdated)
}

func TestStopTrackingDecreasesUptime(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)
	clk.Set(currentTime)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	currentTime = startTime.Add(time.Second)
	clk.Set(currentTime)

	require.NoError(up.StopTracking([]ids.NodeID{nodeID0}))

	up = NewManager(s, &clk)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(clk.UnixTime(), lastUpdated)
}

func TestStopTrackingIncreasesUptime(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)
	clk.Set(currentTime)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	require.NoError(up.Connect(nodeID0))

	currentTime = startTime.Add(time.Second)
	clk.Set(currentTime)

	require.NoError(up.StopTracking([]ids.NodeID{nodeID0}))

	up = NewManager(s, &clk)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Second, duration)
	require.Equal(clk.UnixTime(), lastUpdated)
}

func TestStopTrackingConnectedDBError(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)
	clk := mockable.Clock{}
	up := NewManager(s, &clk)

	require.NoError(up.StartTracking(nil))

	require.NoError(up.Connect(nodeID0))

	s.dbReadError = errTest
	err := up.StopTracking([]ids.NodeID{nodeID0})
	require.ErrorIs(err, errTest)
}

func TestStopTrackingNonConnectedPast(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)
	clk := mockable.Clock{}
	up := NewManager(s, &clk)
	clk.Set(currentTime)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	currentTime = currentTime.Add(-time.Second)
	clk.Set(currentTime)

	require.NoError(up.StopTracking([]ids.NodeID{nodeID0}))

	duration, lastUpdated, err := s.GetUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(startTime.Truncate(time.Second), lastUpdated)
}

func TestStopTrackingNonConnectedDBError(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)
	clk := mockable.Clock{}
	up := NewManager(s, &clk)
	clk.Set(currentTime)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	currentTime = currentTime.Add(time.Second)
	clk.Set(currentTime)

	s.dbWriteError = errTest
	err := up.StopTracking([]ids.NodeID{nodeID0})
	require.ErrorIs(err, errTest)
}

func TestConnectAndDisconnect(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	clk := mockable.Clock{}
	up := NewManager(s, &clk)
	clk.Set(currentTime)

	s.AddNode(nodeID0, startTime)

	connected := up.IsConnected(nodeID0)
	require.False(connected)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	connected = up.IsConnected(nodeID0)
	require.False(connected)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(clk.UnixTime(), lastUpdated)

	require.NoError(up.Connect(nodeID0))

	connected = up.IsConnected(nodeID0)
	require.True(connected)

	currentTime = currentTime.Add(time.Second)
	clk.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Second, duration)
	require.Equal(clk.UnixTime(), lastUpdated)

	require.NoError(up.Disconnect(nodeID0))

	connected = up.IsConnected(nodeID0)
	require.False(connected)

	currentTime = currentTime.Add(time.Second)
	clk.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Second, duration)
	require.Equal(clk.UnixTime(), lastUpdated)
}

func TestConnectAndDisconnectBeforeTracking(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)
	currentTime = currentTime.Add(time.Second)
	clk.Set(currentTime)

	require.NoError(up.Connect(nodeID0))

	currentTime = currentTime.Add(time.Second)
	clk.Set(currentTime)

	require.NoError(up.Disconnect(nodeID0))

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(2*time.Second, duration)
	require.Equal(clk.UnixTime(), lastUpdated)
}

func TestUnrelatedNodeDisconnect(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	nodeID1 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)
	clk.Set(currentTime)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(clk.UnixTime(), lastUpdated)

	require.NoError(up.Connect(nodeID0))

	require.NoError(up.Connect(nodeID1))

	currentTime = currentTime.Add(time.Second)
	clk.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Second, duration)
	require.Equal(clk.UnixTime(), lastUpdated)

	require.NoError(up.Disconnect(nodeID1))

	currentTime = currentTime.Add(time.Second)
	clk.Set(currentTime)

	duration, lastUpdated, err = up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(2*time.Second, duration)
	require.Equal(clk.UnixTime(), lastUpdated)
}

func TestCalculateUptimeWhenNeverTracked(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)

	currentTime := startTime.Add(time.Second)
	clk.Set(currentTime)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Second, duration)
	require.Equal(clk.UnixTime(), lastUpdated)

	uptime, err := up.CalculateUptimePercentFrom(nodeID0, startTime.Truncate(time.Second))
	require.NoError(err)
	require.InDelta(float64(1), uptime, 0)
}

func TestCalculateUptimeWhenNeverConnected(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()

	clk := mockable.Clock{}
	up := NewManager(s, &clk)

	require.NoError(up.StartTracking([]ids.NodeID{}))

	s.AddNode(nodeID0, startTime)

	currentTime := startTime.Add(time.Second)
	clk.Set(currentTime)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(clk.UnixTime(), lastUpdated)

	uptime, err := up.CalculateUptimePercentFrom(nodeID0, startTime)
	require.NoError(err)
	require.InDelta(float64(0), uptime, 0)
}

func TestCalculateUptimeWhenConnectedBeforeTracking(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)
	clk.Set(currentTime)

	require.NoError(up.Connect(nodeID0))

	currentTime = currentTime.Add(time.Second)
	clk.Set(currentTime)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	currentTime = currentTime.Add(time.Second)
	clk.Set(currentTime)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(2*time.Second, duration)
	require.Equal(clk.UnixTime(), lastUpdated)
}

func TestCalculateUptimeWhenConnectedInFuture(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)
	clk.Set(currentTime)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	currentTime = currentTime.Add(2 * time.Second)
	clk.Set(currentTime)

	require.NoError(up.Connect(nodeID0))

	currentTime = currentTime.Add(-time.Second)
	clk.Set(currentTime)

	duration, lastUpdated, err := up.CalculateUptime(nodeID0)
	require.NoError(err)
	require.Equal(time.Duration(0), duration)
	require.Equal(clk.UnixTime(), lastUpdated)
}

func TestCalculateUptimeNonValidator(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	s := NewTestState()

	clk := mockable.Clock{}
	up := NewManager(s, &clk)

	_, err := up.CalculateUptimePercentFrom(nodeID0, startTime)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestCalculateUptimePercentageDivBy0(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)
	clk.Set(currentTime)

	uptime, err := up.CalculateUptimePercentFrom(nodeID0, startTime.Truncate(time.Second))
	require.NoError(err)
	require.InDelta(float64(1), uptime, 0)
}

func TestCalculateUptimePercentage(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	currentTime = currentTime.Add(time.Second)
	clk.Set(currentTime)

	uptime, err := up.CalculateUptimePercentFrom(nodeID0, startTime.Truncate(time.Second))
	require.NoError(err)
	require.InDelta(float64(0), uptime, 0)
}

func TestStopTrackingUnixTimeRegression(t *testing.T) {
	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	currentTime := time.Now()
	startTime := currentTime

	s := NewTestState()
	s.AddNode(nodeID0, startTime)

	clk := mockable.Clock{}
	up := NewManager(s, &clk)
	clk.Set(currentTime)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	require.NoError(up.Connect(nodeID0))

	currentTime = startTime.Add(time.Second)
	clk.Set(currentTime)

	require.NoError(up.StopTracking([]ids.NodeID{nodeID0}))

	currentTime = startTime.Add(time.Second)
	clk.Set(currentTime)

	up = NewManager(s, &clk)

	currentTime = startTime.Add(time.Second)
	clk.Set(currentTime)

	require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

	require.NoError(up.Connect(nodeID0))

	currentTime = startTime.Add(time.Second)
	clk.Set(currentTime)

	perc, err := up.CalculateUptimePercent(nodeID0)
	require.NoError(err)
	require.GreaterOrEqual(float64(1), perc)
}

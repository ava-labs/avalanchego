// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/stretchr/testify/require"
)

func TestPausableManager(t *testing.T) {
	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()
	require := require.New(t)

	// 1: Paused after tracking resumed after tracking
	{
		up, clk, _ := setupTestEnv(nodeID0, startTime)

		// Start tracking
		require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

		currentTime := addTime(clk, startTime, time.Second)
		require.NoError(up.Connect(nodeID0))

		// Pause
		currentTime = addTime(clk, currentTime, time.Second)
		require.NoError(up.Pause(nodeID0))

		// Check uptime
		currentTime = addTime(clk, currentTime, 2*time.Second)
		checkUptime(t, up, nodeID0, 1*time.Second, currentTime)

		// Disconnect and check uptime
		currentTime = addTime(clk, currentTime, 3*time.Second)
		require.NoError(up.Disconnect(nodeID0))
		checkUptime(t, up, nodeID0, 1*time.Second, currentTime)

		// Connect again and check uptime
		currentTime = addTime(clk, currentTime, 4*time.Second)
		require.NoError(up.Connect(nodeID0))
		currentTime = addTime(clk, currentTime, 5*time.Second)
		checkUptime(t, up, nodeID0, 1*time.Second, currentTime)

		// Resume and check uptime
		currentTime = addTime(clk, currentTime, 6*time.Second)
		require.NoError(up.Resume(nodeID0))
		checkUptime(t, up, nodeID0, 1*time.Second, currentTime)

		// Elapsed time check
		currentTime = addTime(clk, currentTime, 7*time.Second)
		checkUptime(t, up, nodeID0, 8*time.Second, currentTime)
	}

	// 2: Paused before tracking resumed after tracking
	{
		up, clk, _ := setupTestEnv(nodeID0, startTime)

		// Pause before tracking
		require.NoError(up.Pause(nodeID0))

		currentTime := addTime(clk, startTime, time.Second)
		require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

		// Connect and check uptime
		currentTime = addTime(clk, currentTime, 1*time.Second)
		require.NoError(up.Connect(nodeID0))

		currentTime = addTime(clk, currentTime, 2*time.Second)
		checkUptime(t, up, nodeID0, 0*time.Second, currentTime)

		// Disconnect and check uptime
		currentTime = addTime(clk, currentTime, 3*time.Second)
		require.NoError(up.Disconnect(nodeID0))
		checkUptime(t, up, nodeID0, 0*time.Second, currentTime)

		// Connect again and resume
		currentTime = addTime(clk, currentTime, 4*time.Second)
		require.NoError(up.Connect(nodeID0))

		currentTime = addTime(clk, currentTime, 5*time.Second)
		require.NoError(up.Resume(nodeID0))

		// Check uptime after resume
		currentTime = addTime(clk, currentTime, 6*time.Second)
		checkUptime(t, up, nodeID0, 6*time.Second, currentTime)
	}

	// 3: Paused after tracking resumed before tracking
	{
		up, clk, s := setupTestEnv(nodeID0, startTime)

		// Start tracking and connect
		require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

		currentTime := addTime(clk, startTime, time.Second)
		require.NoError(up.Connect(nodeID0))

		// Pause and check uptime
		currentTime = addTime(clk, currentTime, 2*time.Second)
		require.NoError(up.Pause(nodeID0))
		checkUptime(t, up, nodeID0, 2*time.Second, currentTime)

		// Stop tracking and reinitialize manager
		currentTime = addTime(clk, currentTime, 3*time.Second)
		require.NoError(up.StopTracking([]ids.NodeID{nodeID0}))
		up = NewPausableManager(NewManager(s, clk))

		checkUptime(t, up, nodeID0, 2*time.Second, currentTime)

		// Resume and check uptime
		currentTime = addTime(clk, currentTime, 5*time.Second)
		require.NoError(up.Resume(nodeID0))
		checkUptime(t, up, nodeID0, 7*time.Second, currentTime)

		// Start tracking and check elapsed time
		currentTime = addTime(clk, currentTime, 6*time.Second)
		require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))
		checkUptime(t, up, nodeID0, 13*time.Second, currentTime)

		currentTime = addTime(clk, currentTime, 7*time.Second)
		checkUptime(t, up, nodeID0, 13*time.Second, currentTime)

		// Connect and final uptime check
		require.NoError(up.Connect(nodeID0))
		currentTime = addTime(clk, currentTime, 8*time.Second)
		checkUptime(t, up, nodeID0, 21*time.Second, currentTime)
	}
}

func setupTestEnv(nodeID ids.NodeID, startTime time.Time) (PausableManager, *mockable.Clock, State) {
	clk := mockable.Clock{}
	clk.Set(startTime)
	s := NewTestState()
	s.AddNode(nodeID, startTime)
	up := NewPausableManager(NewManager(s, &clk))
	return up, &clk, s
}

func addTime(clk *mockable.Clock, currentTime time.Time, duration time.Duration) time.Time {
	newTime := currentTime.Add(duration)
	clk.Set(newTime)
	return newTime
}

func checkUptime(t *testing.T, up PausableManager, nodeID ids.NodeID, expectedUptime time.Duration, expectedLastUpdate time.Time) {
	uptime, lastUpdated, err := up.CalculateUptime(nodeID)
	require.NoError(t, err)
	require.Equal(t, expectedLastUpdate.Unix(), lastUpdated.Unix())
	require.Equal(t, expectedUptime, uptime)
}

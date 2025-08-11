// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/evm/plugin/validators/uptime/interfaces"
)

func TestPausableManager(t *testing.T) {
	vID := ids.GenerateTestID()
	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	tests := []struct {
		name     string
		testFunc func(t *testing.T, up interfaces.PausableManager, clk *mockable.Clock, s uptime.State)
	}{
		{
			name: "Case 1: Connect, pause, start tracking",
			testFunc: func(t *testing.T, up interfaces.PausableManager, clk *mockable.Clock, s uptime.State) {
				require := require.New(t)

				// Connect before tracking
				require.NoError(up.Connect(nodeID0))
				addTime(clk, time.Second)
				wantUptime := 1 * time.Second

				// Pause before tracking
				up.OnValidatorStatusUpdated(vID, nodeID0, false)
				require.True(up.IsPaused(nodeID0))

				// Elapse Time
				addTime(clk, time.Second)
				// Since we have not started tracking this node yet, its observed uptime should
				// be incremented even though it is actually paused.
				wantUptime += 1 * time.Second

				// Start tracking
				require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))
				// Uptime here should not increase after start tracking
				// since the node is still paused after we started tracking
				currentTime := addTime(clk, time.Second)
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Disconnect
				require.NoError(up.Disconnect(nodeID0))
				// Uptime should not have increased
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
			},
		},
		{
			name: "Case 2: Start tracking, connect, pause, re-connect, resume",
			testFunc: func(t *testing.T, up interfaces.PausableManager, clk *mockable.Clock, s uptime.State) {
				require := require.New(t)

				// Start tracking
				require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

				// Connect
				addTime(clk, 1*time.Second)
				require.NoError(up.Connect(nodeID0))

				// Pause
				addTime(clk, 1*time.Second)
				up.OnValidatorStatusUpdated(vID, nodeID0, false)
				require.True(up.IsPaused(nodeID0))

				// Elapse time
				currentTime := addTime(clk, 2*time.Second)
				// Uptime should be 1 second since the node was paused after 1 sec
				wantUptime := 1 * time.Second
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Disconnect and check uptime
				currentTime = addTime(clk, 3*time.Second)
				require.NoError(up.Disconnect(nodeID0))
				// Uptime should not have increased since the node was paused
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Connect again and check uptime
				addTime(clk, 4*time.Second)
				require.NoError(up.Connect(nodeID0))
				currentTime = addTime(clk, 5*time.Second)
				// Uptime should not have increased since the node was paused
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Resume and check uptime
				currentTime = addTime(clk, 6*time.Second)
				up.OnValidatorStatusUpdated(vID, nodeID0, true)
				require.False(up.IsPaused(nodeID0))
				// Uptime should not have increased since the node was paused
				// and we just resumed it
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Elapsed time check
				currentTime = addTime(clk, 7*time.Second)
				// Uptime should increase by 7 seconds above since the node was resumed
				wantUptime += 7 * time.Second
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
			},
		},
		{
			name: "Case 3: Pause, start tracking, connect, re-connect, resume",
			testFunc: func(t *testing.T, up interfaces.PausableManager, clk *mockable.Clock, s uptime.State) {
				require := require.New(t)

				// Pause before tracking
				up.OnValidatorStatusUpdated(vID, nodeID0, false)
				require.True(up.IsPaused(nodeID0))

				// Start tracking
				addTime(clk, time.Second)
				// Uptime should be 1 since the node was paused before we started tracking
				wantUptime := 1 * time.Second
				require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))

				// Connect and check uptime
				addTime(clk, 1*time.Second)
				checkUptime(t, up, nodeID0, wantUptime, clk.Time())
				require.NoError(up.Connect(nodeID0))

				currentTime := addTime(clk, 2*time.Second)
				// Uptime should not have increased since the node was paused after we started tracking
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Disconnect and check uptime
				currentTime = addTime(clk, 3*time.Second)
				require.NoError(up.Disconnect(nodeID0))
				// Uptime should not have increased since the node was paused
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Connect again and resume
				addTime(clk, 4*time.Second)
				require.NoError(up.Connect(nodeID0))
				addTime(clk, 5*time.Second)
				up.OnValidatorStatusUpdated(vID, nodeID0, true)
				require.False(up.IsPaused(nodeID0))

				// Check uptime after resume
				currentTime = addTime(clk, 6*time.Second)
				// Uptime should have increased by 6 seconds since the node was resumed
				wantUptime += 6 * time.Second
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
			},
		},
		{
			name: "Case 4: Start tracking, connect, pause, stop tracking, resume tracking",
			testFunc: func(t *testing.T, up interfaces.PausableManager, clk *mockable.Clock, s uptime.State) {
				require := require.New(t)

				// Start tracking and connect
				require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))
				addTime(clk, time.Second)
				require.NoError(up.Connect(nodeID0))

				// Pause and check uptime
				currentTime := addTime(clk, 2*time.Second)
				up.OnValidatorStatusUpdated(vID, nodeID0, false)
				require.True(up.IsPaused(nodeID0))
				// Uptime should be 2 seconds since the node was paused after 2 seconds
				wantUptime := 2 * time.Second

				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Stop tracking and reinitialize manager
				currentTime = addTime(clk, 3*time.Second)
				require.NoError(up.StopTracking([]ids.NodeID{nodeID0}))
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
				up = NewPausableManager(uptime.NewManager(s, clk))

				// Uptime should not have increased since the node was paused
				// and we have not started tracking again
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Pause and check uptime
				up.OnValidatorStatusUpdated(vID, nodeID0, false)
				require.True(up.IsPaused(nodeID0))
				// Uptime should not have increased since the node was paused
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Resume and check uptime
				currentTime = addTime(clk, 5*time.Second)
				up.OnValidatorStatusUpdated(vID, nodeID0, true)
				require.False(up.IsPaused(nodeID0))
				// Uptime should have increased by 5 seconds since the node was resumed
				wantUptime += 5 * time.Second
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Start tracking and check elapsed time
				currentTime = addTime(clk, 6*time.Second)
				require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))
				// Uptime should have increased by 6 seconds since we started tracking
				// and node was resumed (we assume the node was online until we started tracking)
				wantUptime += 6 * time.Second
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Elapsed time
				currentTime = addTime(clk, 7*time.Second)
				// Uptime should not have increased since the node was not connected
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Connect and final uptime check
				require.NoError(up.Connect(nodeID0))
				currentTime = addTime(clk, 8*time.Second)
				// Uptime should have increased by 8 seconds since the node was connected
				wantUptime += 8 * time.Second
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
			},
		},
		{
			name: "Case 5: Node paused after we stop tracking",
			testFunc: func(t *testing.T, up interfaces.PausableManager, clk *mockable.Clock, s uptime.State) {
				require := require.New(t)

				// Start tracking and connect
				require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))
				addTime(clk, time.Second)
				require.NoError(up.Connect(nodeID0))

				// Stop tracking
				currentTime := addTime(clk, 2*time.Second)
				wantUptime := 2 * time.Second
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
				require.NoError(up.StopTracking([]ids.NodeID{nodeID0}))

				// Pause after a while
				addTime(clk, 3*time.Second)
				// wantUptime should increase since we stopped tracking
				wantUptime += 3 * time.Second
				up.OnValidatorStatusUpdated(vID, nodeID0, false)
				require.True(up.IsPaused(nodeID0))
				// wantUptime should increase since we stopped tracking (even if the node was paused)
				currentTime = addTime(clk, 4*time.Second)
				wantUptime += 4 * time.Second

				// Start tracking and check elapsed time
				require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))
				// Uptime have increased since the node was paused before we started tracking
				// We should be optimistic and assume the node was online and active until we start tracking
				require.True(up.IsPaused(nodeID0))
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
			},
		},
		{
			name: "Case 6: Paused node got resumed after we stop tracking",
			testFunc: func(t *testing.T, up interfaces.PausableManager, clk *mockable.Clock, s uptime.State) {
				require := require.New(t)

				// Start tracking and connect
				require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))
				addTime(clk, time.Second)
				require.NoError(up.Connect(nodeID0))

				// Pause after a while
				currentTime := addTime(clk, 2*time.Second)
				// wantUptime should increase
				wantUptime := 2 * time.Second
				up.OnValidatorStatusUpdated(vID, nodeID0, false)
				require.True(up.IsPaused(nodeID0))
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Stop tracking
				currentTime = addTime(clk, 3*time.Second)
				// wantUptime should not increase since the node was paused
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
				require.NoError(up.StopTracking([]ids.NodeID{nodeID0}))

				// Resume after a while
				addTime(clk, 4*time.Second)
				// wantUptime should increase since we stopped tracking
				wantUptime += 4 * time.Second
				up.OnValidatorStatusUpdated(vID, nodeID0, true)
				require.False(up.IsPaused(nodeID0))
				// wantUptime should increase since we stopped tracking
				currentTime = addTime(clk, 5*time.Second)
				wantUptime += 5 * time.Second

				// Start tracking and check elapsed time
				require.NoError(up.StartTracking([]ids.NodeID{nodeID0}))
				// Uptime should have increased by 4 seconds since the node was resumed
				// We should be optimistic and assume the node was online and active until we start tracking
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			up, clk, s := setupTestEnv(nodeID0, startTime)
			test.testFunc(t, up, clk, s)
		})
	}
}

func setupTestEnv(nodeID ids.NodeID, startTime time.Time) (interfaces.PausableManager, *mockable.Clock, uptime.State) {
	clk := mockable.Clock{}
	clk.Set(startTime)
	s := uptime.NewTestState()
	s.AddNode(nodeID, startTime)
	up := NewPausableManager(uptime.NewManager(s, &clk))
	return up, &clk, s
}

func addTime(clk *mockable.Clock, duration time.Duration) time.Time {
	clk.Set(clk.Time().Add(duration))
	return clk.Time()
}

func checkUptime(t *testing.T, up interfaces.PausableManager, nodeID ids.NodeID, wantUptime time.Duration, wantLastUpdate time.Time) {
	t.Helper()
	uptime, lastUpdated, err := up.CalculateUptime(nodeID)
	require.NoError(t, err)
	require.Equal(t, wantLastUpdate.Unix(), lastUpdated.Unix())
	require.Equal(t, wantUptime, uptime)
}

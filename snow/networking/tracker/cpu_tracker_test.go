// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/uptime"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestCPUTracker(t *testing.T) {
	halflife := time.Second
	validators := validators.NewSet()

	cpuTracker, err := NewCPUTracker(prometheus.NewRegistry(), uptime.ContinuousFactory{}, halflife, validators)
	assert.NoError(t, err)

	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}

	startTime1 := time.Now()
	endTime1 := startTime1.Add(halflife)

	cpuTracker.StartCPU(vdr1, startTime1)
	cpuTracker.StopCPU(vdr1, endTime1)

	startTime2 := endTime1
	endTime2 := startTime2.Add(halflife)
	cpuTracker.StartCPU(vdr2, startTime2)
	cpuTracker.StopCPU(vdr2, endTime2)

	utilization1 := cpuTracker.Utilization(vdr1, endTime2)
	utilization2 := cpuTracker.Utilization(vdr2, endTime2)

	if utilization1 >= utilization2 {
		t.Fatalf("Utilization should have been higher for the more recent spender")
	}

	cumulative := cpuTracker.CumulativeUtilization(endTime2)
	sum := utilization1 + utilization2
	if cumulative != sum {
		t.Fatalf("Cumulative utilization: %f should have been equal to the sum of the spenders: %f", cumulative, sum)
	}

	expectedLen := 2
	len := cpuTracker.Len()
	if len != expectedLen {
		t.Fatalf("Expected length to match number of spenders: %d, but found length: %d", expectedLen, len)
	}

	// Set pruning time to 64 halflifes in the future, to guarantee that
	// any counts should have gone to 0
	pruningTime := endTime2.Add(halflife * 64)
	cpuTracker.CumulativeUtilization(pruningTime)
	len = cpuTracker.Len()
	if len != 0 {
		t.Fatalf("Expected length to be 0 after pruning, but found length: %d", len)
	}
}

func TestCPUTrackerCallbacks(t *testing.T) {
	halflife := time.Second
	validators := validators.NewSet()
	nodeID1, nodeID2, nodeID3 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	if err := validators.AddWeight(nodeID1, 1); err != nil {
		t.Fatal(err)
	}
	if err := validators.AddWeight(nodeID2, 2); err != nil {
		t.Fatal(err)
	}

	val, err := NewCPUTracker(prometheus.NewRegistry(), uptime.ContinuousFactory{}, halflife, validators)
	assert.NoError(t, err)

	cpuTracker := val.(*cpuTracker)
	startTime1 := time.Now()
	endTime1 := startTime1.Add(halflife)

	cpuTracker.StartCPU(nodeID1, startTime1)
	assert.Equal(t, uint64(1), cpuTracker.ActiveWeight())
	cpuTracker.StopCPU(nodeID1, endTime1)

	startTime2 := endTime1
	endTime2 := startTime2.Add(halflife)
	cpuTracker.StartCPU(nodeID2, startTime2)
	assert.Equal(t, uint64(3), cpuTracker.ActiveWeight())
	cpuTracker.StopCPU(nodeID2, endTime2)

	// change the weight while nodeID1 is active and nodeID2 is not
	startTime := time.Now()
	endTime := startTime1.Add(halflife)
	cpuTracker.StartCPU(nodeID1, startTime)
	if err := validators.AddWeight(nodeID1, 3); err != nil {
		t.Fatal(err)
	}
	if err := validators.AddWeight(nodeID3, 5); err != nil {
		t.Fatal(err)
	}
	// only nodeID1's weight should be reflected, but nodeID3 shouldn't since it isn't active
	assert.Equal(t, uint64(6), cpuTracker.ActiveWeight())
	cpuTracker.StopCPU(nodeID1, endTime)

	// reduce the node's weight
	if err := validators.RemoveWeight(nodeID1, 1); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, uint64(5), cpuTracker.ActiveWeight())

	// remove the node completely
	if err := validators.RemoveWeight(nodeID1, 3); err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, uint64(2), cpuTracker.ActiveWeight())
}

func TestCPUTrackerTimeUntilUtilization(t *testing.T) {
	halflife := 5 * time.Second
	cpuTracker, err := NewCPUTracker(prometheus.NewRegistry(), uptime.ContinuousFactory{}, halflife, validators.NewSet())
	assert.NoError(t, err)
	now := time.Now()
	nodeID := ids.GenerateTestNodeID()
	// Start the meter
	cpuTracker.StartCPU(nodeID, now)
	// One halflife passes; stop the meter
	now = now.Add(halflife)
	cpuTracker.StopCPU(nodeID, now)
	// Read the current value
	currentVal := cpuTracker.Utilization(nodeID, now)
	// Suppose we want to wait for the value to be
	// a third of its current value
	desiredVal := currentVal / 3
	// See when that should happen
	timeUntilDesiredVal := cpuTracker.TimeUntilUtilization(nodeID, now, desiredVal)
	// Get the actual value at that time
	now = now.Add(timeUntilDesiredVal)
	actualVal := cpuTracker.Utilization(nodeID, now)
	// Make sure the actual/expected are close
	assert.InDelta(t, desiredVal, actualVal, .00001)
	// Make sure TimeUntilUtilization returns the zero duration if
	// the value provided >= the current value
	assert.Zero(t, cpuTracker.TimeUntilUtilization(nodeID, now, actualVal))
	assert.Zero(t, cpuTracker.TimeUntilUtilization(nodeID, now, actualVal+.1))
	// Make sure it returns the zero duration if the node isn't known
	assert.Zero(t, cpuTracker.TimeUntilUtilization(ids.GenerateTestNodeID(), now, 0.0001))
}

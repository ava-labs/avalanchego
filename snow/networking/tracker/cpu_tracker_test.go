// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/uptime"
)

func TestCPUTracker(t *testing.T) {
	halflife := time.Second
	cpuTracker := NewCPUTracker(uptime.ContinuousFactory{}, halflife)
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

func TestCPUTrackerTimeUntilUtilization(t *testing.T) {
	halflife := 5 * time.Second
	cpuTracker := NewCPUTracker(uptime.ContinuousFactory{}, halflife)
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

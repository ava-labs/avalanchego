// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/cpu"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestNewCPUTracker(t *testing.T) {
	assert := assert.New(t)

	reg := prometheus.NewRegistry()
	halflife := 5 * time.Second
	factory := &meter.ContinuousFactory{}

	trackerIntf, err := NewCPUTracker(reg, cpu.NoUsage, factory, halflife)
	assert.NoError(err)
	tracker, ok := trackerIntf.(*cpuTracker)
	assert.True(ok)
	assert.Equal(factory, tracker.factory)
	assert.NotNil(tracker.cumulativeMeter)
	assert.Equal(halflife, tracker.halflife)
	assert.NotNil(tracker.meters)
	assert.NotNil(tracker.metrics)
}

func TestCPUTracker(t *testing.T) {
	halflife := 5 * time.Second

	ctrl := gomock.NewController(t)
	mockUser := cpu.NewMockUser(ctrl)
	mockUser.EXPECT().Usage().Return(1.0).Times(3)

	cpuTracker, err := NewCPUTracker(prometheus.NewRegistry(), mockUser, meter.ContinuousFactory{}, time.Second)
	assert.NoError(t, err)

	node1 := ids.NodeID{1}
	node2 := ids.NodeID{2}

	// Note that all the durations between start and end are [halflife].
	startTime1 := time.Now()
	endTime1 := startTime1.Add(halflife)
	// Note that all CPU usage is attributed to at-large allocation.
	cpuTracker.IncCPU(node1, startTime1)
	cpuTracker.DecCPU(node1, endTime1)

	startTime2 := endTime1
	endTime2 := startTime2.Add(halflife)
	// Note that all CPU usage is attributed to at-large allocation.
	cpuTracker.IncCPU(node2, startTime2)
	cpuTracker.DecCPU(node2, endTime2)

	node1Utilization := cpuTracker.Utilization(node1, endTime2)
	node2Utilization := cpuTracker.Utilization(node2, endTime2)
	if node1Utilization >= node2Utilization {
		t.Fatalf("Utilization should have been higher for the more recent spender")
	}

	cumulative := cpuTracker.CumulativeUtilization()
	sum := node1Utilization + node2Utilization
	if cumulative != sum {
		t.Fatalf("Cumulative utilization: %f should have been equal to the sum of the spenders: %f", cumulative, sum)
	}

	mockUser.EXPECT().Usage().Return(.5).Times(3)

	startTime3 := endTime2
	endTime3 := startTime3.Add(halflife)
	newNode1Utilization := cpuTracker.Utilization(node1, endTime3)
	if newNode1Utilization >= node1Utilization {
		t.Fatalf("node CPU utilization should decrease over time")
	}
	newCumulative := cpuTracker.CumulativeUtilization()
	if newCumulative >= cumulative {
		t.Fatal("at-large CPU utilization should decrease over time ")
	}

	startTime4 := endTime3
	endTime4 := startTime4.Add(halflife)
	// Note that only half of CPU usage is attributed to at-large allocation.
	cpuTracker.IncCPU(node1, startTime4)
	cpuTracker.DecCPU(node1, endTime4)

	cumulative = cpuTracker.CumulativeUtilization()
	sum = node1Utilization + node2Utilization
	if cumulative >= sum {
		t.Fatal("Sum of CPU usage should exceed cumulative at-large utilization")
	}
}

func TestCPUTrackerTimeUntilUtilization(t *testing.T) {
	halflife := 5 * time.Second
	cpuTracker, err := NewCPUTracker(prometheus.NewRegistry(), cpu.NoUsage, meter.ContinuousFactory{}, halflife)
	assert.NoError(t, err)
	now := time.Now()
	nodeID := ids.GenerateTestNodeID()
	// Start the meter
	cpuTracker.IncCPU(nodeID, now)
	// One halflife passes; stop the meter
	now = now.Add(halflife)
	cpuTracker.DecCPU(nodeID, now)
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

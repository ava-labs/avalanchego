// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/resource/resourcemock"
)

func TestNewCPUTracker(t *testing.T) {
	require := require.New(t)

	reg := prometheus.NewRegistry()
	halflife := 5 * time.Second
	factory := &meter.ContinuousFactory{}

	trackerIntf, err := NewResourceTracker(reg, resource.NoUsage, factory, halflife)
	require.NoError(err)
	require.IsType(&resourceTracker{}, trackerIntf)
	tracker := trackerIntf.(*resourceTracker)
	require.Equal(factory, tracker.factory)
	require.NotNil(tracker.processingMeter)
	require.Equal(halflife, tracker.halflife)
	require.NotNil(tracker.meters)
	require.NotNil(tracker.metrics)
}

func TestCPUTracker(t *testing.T) {
	require := require.New(t)

	halflife := 5 * time.Second

	ctrl := gomock.NewController(t)
	mockUser := resourcemock.NewUser(ctrl)
	mockUser.EXPECT().CPUUsage().Return(1.0).Times(3)

	tracker, err := NewResourceTracker(prometheus.NewRegistry(), mockUser, meter.ContinuousFactory{}, time.Second)
	require.NoError(err)

	node1 := ids.BuildTestNodeID([]byte{1})
	node2 := ids.BuildTestNodeID([]byte{2})

	// Note that all the durations between start and end are [halflife].
	startTime1 := time.Now()
	endTime1 := startTime1.Add(halflife)
	// Note that all CPU usage is attributed to at-large allocation.
	tracker.StartProcessing(node1, startTime1)
	tracker.StopProcessing(node1, endTime1)

	startTime2 := endTime1
	endTime2 := startTime2.Add(halflife)
	// Note that all CPU usage is attributed to at-large allocation.
	tracker.StartProcessing(node2, startTime2)
	tracker.StopProcessing(node2, endTime2)

	cpuTracker := tracker.CPUTracker()

	node1Utilization := cpuTracker.Usage(node1, endTime2)
	node2Utilization := cpuTracker.Usage(node2, endTime2)
	require.Greater(node2Utilization, node1Utilization)

	cumulative := cpuTracker.TotalUsage()
	sum := node1Utilization + node2Utilization
	require.Equal(sum, cumulative)

	mockUser.EXPECT().CPUUsage().Return(.5).Times(3)

	startTime3 := endTime2
	endTime3 := startTime3.Add(halflife)
	newNode1Utilization := cpuTracker.Usage(node1, endTime3)
	require.Greater(node1Utilization, newNode1Utilization)
	newCumulative := cpuTracker.TotalUsage()
	require.Greater(cumulative, newCumulative)

	startTime4 := endTime3
	endTime4 := startTime4.Add(halflife)
	// Note that only half of CPU usage is attributed to at-large allocation.
	tracker.StartProcessing(node1, startTime4)
	tracker.StopProcessing(node1, endTime4)

	cumulative = cpuTracker.TotalUsage()
	sum = node1Utilization + node2Utilization
	require.Greater(sum, cumulative)
}

func TestCPUTrackerTimeUntilCPUUtilization(t *testing.T) {
	require := require.New(t)

	halflife := 5 * time.Second
	tracker, err := NewResourceTracker(prometheus.NewRegistry(), resource.NoUsage, meter.ContinuousFactory{}, halflife)
	require.NoError(err)
	now := time.Now()
	nodeID := ids.GenerateTestNodeID()
	// Start the meter
	tracker.StartProcessing(nodeID, now)
	// One halflife passes; stop the meter
	now = now.Add(halflife)
	tracker.StopProcessing(nodeID, now)
	cpuTracker := tracker.CPUTracker()
	// Read the current value
	currentVal := cpuTracker.Usage(nodeID, now)
	// Suppose we want to wait for the value to be
	// a third of its current value
	desiredVal := currentVal / 3
	// See when that should happen
	timeUntilDesiredVal := cpuTracker.TimeUntilUsage(nodeID, now, desiredVal)
	// Get the actual value at that time
	now = now.Add(timeUntilDesiredVal)
	actualVal := cpuTracker.Usage(nodeID, now)
	// Make sure the actual/expected are close
	require.InDelta(desiredVal, actualVal, .00001)
	// Make sure TimeUntilUsage returns the zero duration if
	// the value provided >= the current value
	require.Zero(cpuTracker.TimeUntilUsage(nodeID, now, actualVal))
	require.Zero(cpuTracker.TimeUntilUsage(nodeID, now, actualVal+.1))
	// Make sure it returns the zero duration if the node isn't known
	require.Zero(cpuTracker.TimeUntilUsage(ids.GenerateTestNodeID(), now, 0.0001))
}

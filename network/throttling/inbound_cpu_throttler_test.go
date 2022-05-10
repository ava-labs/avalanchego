// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/uptime"
)

func TestNewCPUThrottler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	assert := assert.New(t)
	reg := prometheus.NewRegistry()
	clock := mockable.Clock{}
	clock.Set(time.Now())
	vdrs := validators.NewSet()
	cpuTracker, err := tracker.NewCPUTracker(reg, uptime.ContinuousFactory{}, time.Second, vdrs)
	assert.NoError(err)
	config := CPUThrottlerConfig{
		Clock:           clock,
		MaxRecheckDelay: time.Second,
	}
	cpuTargeter := tracker.NewMockCPUTargeter(ctrl)
	throttlerIntf, err := NewCPUThrottler("", reg, config, vdrs, cpuTracker, cpuTargeter)
	assert.NoError(err)
	throttler, ok := throttlerIntf.(*cpuThrottler)
	assert.True(ok)
	assert.EqualValues(config, throttler.CPUThrottlerConfig)
	assert.EqualValues(cpuTracker, throttler.cpuTracker)
	assert.NotNil(throttler.cpuTargeter)
}

func TestCPUThrottler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	assert := assert.New(t)

	// Setup
	cpuTracker := tracker.NewMockTimeTracker(ctrl)
	maxRecheckDelay := 100 * time.Millisecond
	config := CPUThrottlerConfig{
		MaxRecheckDelay: maxRecheckDelay,
	}
	vdrs := validators.NewSet()
	vdrID, nonVdrID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	err := vdrs.AddWeight(vdrID, 1)
	assert.NoError(err)
	cpuTargeter := tracker.NewMockCPUTargeter(ctrl)
	cpuThrottler, err := NewCPUThrottler("", prometheus.NewRegistry(), config, vdrs, cpuTracker, cpuTargeter)
	assert.NoError(err)

	// Case: Actual CPU <= target CPU; should return immediately
	// for both validator and non-validator
	cpuTargeter.EXPECT().TargetCPUUsage(vdrID).Return(1.0).Times(1)
	cpuTracker.EXPECT().Utilization(vdrID, gomock.Any()).Return(float64(0.9)).Times(1)
	cpuTargeter.EXPECT().TargetCPUUsage(nonVdrID).Return(1.0).Times(1)
	cpuTracker.EXPECT().Utilization(nonVdrID, gomock.Any()).Return(float64(0.9)).Times(1)
	onAcquire := make(chan struct{})
	// Check for validator
	go func() {
		cpuThrottler.Acquire(context.Background(), vdrID)
		onAcquire <- struct{}{}
	}()
	<-onAcquire
	// Check for non-validator
	go func() {
		cpuThrottler.Acquire(context.Background(), nonVdrID)
		onAcquire <- struct{}{}
	}()
	<-onAcquire

	// Case: Actual CPU usage > target CPU usage; we should wait.
	// In the first loop iteration inside acquire,
	// say the actual CPU usage exceeds the target.
	cpuTargeter.EXPECT().TargetCPUUsage(vdrID).Return(float64(0)).Times(1)
	cpuTracker.EXPECT().Utilization(vdrID, gomock.Any()).Return(float64(1)).Times(1)
	cpuTargeter.EXPECT().TargetCPUUsage(nonVdrID).Return(float64(0)).Times(1)
	cpuTracker.EXPECT().Utilization(nonVdrID, gomock.Any()).Return(float64(1)).Times(1)
	// Note we'll only actually wait [maxRecheckDelay]. We set [timeUntilAtCPUTarget]
	// much larger to assert that the min recheck frequency is honored below.
	timeUntilAtCPUTarget := 100 * maxRecheckDelay
	cpuTracker.EXPECT().TimeUntilUtilization(vdrID, gomock.Any(), gomock.Any()).Return(timeUntilAtCPUTarget).Times(1)
	cpuTracker.EXPECT().TimeUntilUtilization(nonVdrID, gomock.Any(), gomock.Any()).Return(timeUntilAtCPUTarget).Times(1)

	// The second iteration, say the CPU usage is OK.
	cpuTargeter.EXPECT().TargetCPUUsage(vdrID).Return(float64(1)).Times(1)
	cpuTracker.EXPECT().Utilization(vdrID, gomock.Any()).Return(float64(0)).Times(1)
	cpuTargeter.EXPECT().TargetCPUUsage(nonVdrID).Return(float64(1)).Times(1)
	cpuTracker.EXPECT().Utilization(nonVdrID, gomock.Any()).Return(float64(0)).Times(1)

	// Check for validator
	go func() {
		cpuThrottler.Acquire(context.Background(), vdrID)
		onAcquire <- struct{}{}
	}()
	// Make sure the min re-check frequency is honored
	select {
	// Use 5*maxRecheckDelay and not just maxRecheckDelay to give a buffer
	// and avoid flakiness. If the min re-check freq isn't honored,
	// we'll wait [timeUntilAtCPUTarget].
	case <-time.After(5 * maxRecheckDelay):
		assert.FailNow("should have returned after about [maxRecheckDelay]")
	case <-onAcquire:
	}

	// Check for non-validator
	go func() {
		cpuThrottler.Acquire(context.Background(), nonVdrID)
		onAcquire <- struct{}{}
	}()
	// Make sure the min re-check frequency is honored
	select {
	// Use 5*maxRecheckDelay and not just maxRecheckDelay to give a buffer
	// and avoid flakiness. If the min re-check freq isn't honored,
	// we'll wait [timeUntilAtCPUTarget].
	case <-time.After(5 * maxRecheckDelay):
		assert.FailNow("should have returned after about [maxRecheckDelay]")
	case <-onAcquire:
	}
}

func TestCPUThrottlerContextCancel(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Setup
	cpuTracker := tracker.NewMockTimeTracker(ctrl)
	maxRecheckDelay := 10 * time.Second
	config := CPUThrottlerConfig{
		MaxRecheckDelay: maxRecheckDelay,
	}
	vdrs := validators.NewSet()
	vdrID := ids.GenerateTestNodeID()
	err := vdrs.AddWeight(vdrID, 1)
	assert.NoError(err)
	cpuTargeter := tracker.NewMockCPUTargeter(ctrl)
	cpuThrottler, err := NewCPUThrottler("", prometheus.NewRegistry(), config, vdrs, cpuTracker, cpuTargeter)
	assert.NoError(err)

	// Case: Actual CPU usage > target CPU usage; we should wait.
	// Mock the CPU tracker so that the first loop iteration inside acquire,
	// it says the actual CPU usage exceeds the target.
	// There should be no second iteration because we've already returned.
	cpuTargeter.EXPECT().TargetCPUUsage(vdrID).Return(float64(0)).Times(1)
	cpuTracker.EXPECT().Utilization(vdrID, gomock.Any()).Return(float64(1)).Times(1)
	cpuTracker.EXPECT().TimeUntilUtilization(vdrID, gomock.Any(), gomock.Any()).Return(maxRecheckDelay).Times(1)
	onAcquire := make(chan struct{})
	// Pass a canceled context into Acquire so that it returns immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	go func() {
		cpuThrottler.Acquire(ctx, vdrID)
		onAcquire <- struct{}{}
	}()
	select {
	case <-onAcquire:
	case <-time.After(maxRecheckDelay / 2):
		// Make sure Acquire returns well before the second check (i.e. "immediately")
		assert.Fail("should have returned immediately")
	}
}

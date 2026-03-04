// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/networking/tracker/trackermock"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

func TestNewSystemThrottler(t *testing.T) {
	ctrl := gomock.NewController(t)
	require := require.New(t)
	reg := prometheus.NewRegistry()
	clock := mockable.Clock{}
	clock.Set(time.Now())
	resourceTracker, err := tracker.NewResourceTracker(reg, resource.NoUsage, meter.ContinuousFactory{}, time.Second)
	require.NoError(err)
	cpuTracker := resourceTracker.CPUTracker()

	config := SystemThrottlerConfig{
		Clock:           clock,
		MaxRecheckDelay: time.Second,
	}
	targeter := trackermock.NewTargeter(ctrl)
	throttlerIntf, err := NewSystemThrottler("", reg, config, cpuTracker, targeter)
	require.NoError(err)
	require.IsType(&systemThrottler{}, throttlerIntf)
	throttler := throttlerIntf.(*systemThrottler)
	require.Equal(clock, config.Clock)
	require.Equal(time.Second, config.MaxRecheckDelay)
	require.Equal(cpuTracker, throttler.tracker)
	require.Equal(targeter, throttler.targeter)
}

func TestSystemThrottler(t *testing.T) {
	ctrl := gomock.NewController(t)
	require := require.New(t)

	// Setup
	mockTracker := trackermock.NewTracker(ctrl)
	maxRecheckDelay := 100 * time.Millisecond
	config := SystemThrottlerConfig{
		MaxRecheckDelay: maxRecheckDelay,
	}
	vdrID, nonVdrID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	targeter := trackermock.NewTargeter(ctrl)
	throttler, err := NewSystemThrottler("", prometheus.NewRegistry(), config, mockTracker, targeter)
	require.NoError(err)

	// Case: Actual usage <= target usage; should return immediately
	// for both validator and non-validator
	targeter.EXPECT().TargetUsage(vdrID).Return(1.0).Times(1)
	mockTracker.EXPECT().Usage(vdrID, gomock.Any()).Return(0.9).Times(1)

	throttler.Acquire(t.Context(), vdrID)

	targeter.EXPECT().TargetUsage(nonVdrID).Return(1.0).Times(1)
	mockTracker.EXPECT().Usage(nonVdrID, gomock.Any()).Return(0.9).Times(1)

	throttler.Acquire(t.Context(), nonVdrID)

	// Case: Actual usage > target usage; we should wait.
	// In the first loop iteration inside acquire,
	// say the actual usage exceeds the target.
	targeter.EXPECT().TargetUsage(vdrID).Return(0.0).Times(1)
	mockTracker.EXPECT().Usage(vdrID, gomock.Any()).Return(1.0).Times(1)
	// Note we'll only actually wait [maxRecheckDelay]. We set [timeUntilAtDiskTarget]
	// much larger to assert that the min recheck frequency is honored below.
	timeUntilAtDiskTarget := 100 * maxRecheckDelay
	mockTracker.EXPECT().TimeUntilUsage(vdrID, gomock.Any(), gomock.Any()).Return(timeUntilAtDiskTarget).Times(1)

	// The second iteration, say the usage is OK.
	targeter.EXPECT().TargetUsage(vdrID).Return(1.0).Times(1)
	mockTracker.EXPECT().Usage(vdrID, gomock.Any()).Return(0.0).Times(1)

	onAcquire := make(chan struct{})

	// Check for validator
	go func() {
		throttler.Acquire(t.Context(), vdrID)
		onAcquire <- struct{}{}
	}()
	// Make sure the min re-check frequency is honored
	select {
	// Use 5*maxRecheckDelay and not just maxRecheckDelay to give a buffer
	// and avoid flakiness. If the min re-check freq isn't honored,
	// we'll wait [timeUntilAtDiskTarget].
	case <-time.After(5 * maxRecheckDelay):
		require.FailNow("should have returned after about [maxRecheckDelay]")
	case <-onAcquire:
	}

	targeter.EXPECT().TargetUsage(nonVdrID).Return(0.0).Times(1)
	mockTracker.EXPECT().Usage(nonVdrID, gomock.Any()).Return(1.0).Times(1)

	mockTracker.EXPECT().TimeUntilUsage(nonVdrID, gomock.Any(), gomock.Any()).Return(timeUntilAtDiskTarget).Times(1)

	targeter.EXPECT().TargetUsage(nonVdrID).Return(1.0).Times(1)
	mockTracker.EXPECT().Usage(nonVdrID, gomock.Any()).Return(0.0).Times(1)

	// Check for non-validator
	go func() {
		throttler.Acquire(t.Context(), nonVdrID)
		onAcquire <- struct{}{}
	}()
	// Make sure the min re-check frequency is honored
	select {
	// Use 5*maxRecheckDelay and not just maxRecheckDelay to give a buffer
	// and avoid flakiness. If the min re-check freq isn't honored,
	// we'll wait [timeUntilAtDiskTarget].
	case <-time.After(5 * maxRecheckDelay):
		require.FailNow("should have returned after about [maxRecheckDelay]")
	case <-onAcquire:
	}
}

func TestSystemThrottlerContextCancel(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	// Setup
	mockTracker := trackermock.NewTracker(ctrl)
	maxRecheckDelay := 10 * time.Second
	config := SystemThrottlerConfig{
		MaxRecheckDelay: maxRecheckDelay,
	}
	vdrID := ids.GenerateTestNodeID()
	targeter := trackermock.NewTargeter(ctrl)
	throttler, err := NewSystemThrottler("", prometheus.NewRegistry(), config, mockTracker, targeter)
	require.NoError(err)

	// Case: Actual usage > target usage; we should wait.
	// Mock the tracker so that the first loop iteration inside acquire,
	// it says the actual usage exceeds the target.
	// There should be no second iteration because we've already returned.
	targeter.EXPECT().TargetUsage(vdrID).Return(0.0).Times(1)
	mockTracker.EXPECT().Usage(vdrID, gomock.Any()).Return(1.0).Times(1)
	mockTracker.EXPECT().TimeUntilUsage(vdrID, gomock.Any(), gomock.Any()).Return(maxRecheckDelay).Times(1)
	onAcquire := make(chan struct{})
	// Pass a canceled context into Acquire so that it returns immediately.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	go func() {
		throttler.Acquire(ctx, vdrID)
		onAcquire <- struct{}{}
	}()
	select {
	case <-onAcquire:
	case <-time.After(maxRecheckDelay / 2):
		// Make sure Acquire returns well before the second check (i.e. "immediately")
		require.Fail("should have returned immediately")
	}
}

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestCPUTarget(t *testing.T) {
	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdrs := validators.NewSet()
	if err := vdrs.AddWeight(vdr1, 1); err != nil {
		t.Fatal(err)
	}

	ctrl := gomock.NewController(t)
	cpuTracker := NewMockTimeTracker(ctrl)
	cpuTracker.EXPECT().CumulativeUtilization(gomock.Any()).Return(10.0).Times(2)
	cpuTracker.EXPECT().Len().Return(1).AnyTimes()
	cpuTracker.EXPECT().ActiveWeight().Return(uint64(1)).AnyTimes()

	cpuTarget, err := NewCPUTargeter(prometheus.NewRegistry(), &CPUTargeterConfig{CPUTarget: 2, VdrCPUPercentage: .5, MaxScaling: 20, SinglePeerMaxUsagePercentage: 1}, vdrs, cpuTracker)
	assert.NoError(t, err)
	highUsage1 := cpuTarget.TargetCPUUsage(vdr1)
	highUsage2 := cpuTarget.TargetCPUUsage(vdr2)

	cpuTracker.EXPECT().CumulativeUtilization(gomock.Any()).Return(1.0).Times(2)
	mediumUsage1 := cpuTarget.TargetCPUUsage(vdr1)
	mediumUsage2 := cpuTarget.TargetCPUUsage(vdr2)

	cpuTracker.EXPECT().CumulativeUtilization(gomock.Any()).Return(0.1).Times(2)
	lowUsage1 := cpuTarget.TargetCPUUsage(vdr1)
	lowUsage2 := cpuTarget.TargetCPUUsage(vdr2)

	assert.Less(t, highUsage1, mediumUsage1)
	assert.Less(t, mediumUsage1, lowUsage1)

	assert.Less(t, highUsage2, mediumUsage2)
	assert.Less(t, mediumUsage2, lowUsage2)
}

// ensure that there is a maximum for the scaled target
func TestCPUTargetMaxScaling(t *testing.T) {
	vdr1 := ids.NodeID{1}
	vdrs := validators.NewSet()
	if err := vdrs.AddWeight(vdr1, 1); err != nil {
		t.Fatal(err)
	}

	ctrl := gomock.NewController(t)
	cpuTracker := NewMockTimeTracker(ctrl)

	// 0 usage should trigger the maximum amount of scaling
	cpuTracker.EXPECT().CumulativeUtilization(gomock.Any()).Return(0.0).Times(1)

	// set Len, ActiveWeight, and VdrCPUPercentage to 1 so that the single validator will get 100% of the scaled target
	cpuTracker.EXPECT().Len().Return(1).AnyTimes()
	cpuTracker.EXPECT().ActiveWeight().Return(uint64(1)).AnyTimes()
	cpuTarget, err := NewCPUTargeter(prometheus.NewRegistry(), &CPUTargeterConfig{CPUTarget: 2, VdrCPUPercentage: 1, MaxScaling: 20, SinglePeerMaxUsagePercentage: 1}, vdrs, cpuTracker)
	assert.NoError(t, err)
	assert.Equal(t, float64(40), cpuTarget.TargetCPUUsage(vdr1))
}

// ensure that having 0 weight doesn't cause a divide by 0 error
func TestCPUTarget0Weight(t *testing.T) {
	vdr1 := ids.NodeID{1}
	vdrs := validators.NewSet()
	if err := vdrs.AddWeight(vdr1, 1); err != nil {
		t.Fatal(err)
	}
	ctrl := gomock.NewController(t)
	cpuTracker := NewMockTimeTracker(ctrl)
	cpuTracker.EXPECT().CumulativeUtilization(gomock.Any()).Return(2.0).Times(1)
	cpuTracker.EXPECT().Len().Return(1).AnyTimes()
	cpuTracker.EXPECT().ActiveWeight().Return(uint64(0))

	cpuTarget, err := NewCPUTargeter(prometheus.NewRegistry(), &CPUTargeterConfig{CPUTarget: 2, VdrCPUPercentage: 1, MaxScaling: 20, SinglePeerMaxUsagePercentage: 1}, vdrs, cpuTracker)
	assert.NoError(t, err)
	scaledTarget := cpuTarget.TargetCPUUsage(vdr1)

	assert.Equal(t, float64(2), scaledTarget)
}

func TestCPUTargetActiveWeightScaling(t *testing.T) {
	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdrs := validators.NewSet()
	if err := vdrs.AddWeight(vdr1, 1); err != nil {
		t.Fatal(err)
	}
	ctrl := gomock.NewController(t)
	cpuTracker := NewMockTimeTracker(ctrl)
	cpuTracker.EXPECT().CumulativeUtilization(gomock.Any()).Return(2.0).AnyTimes()
	cpuTracker.EXPECT().Len().Return(1).AnyTimes()
	cpuTracker.EXPECT().ActiveWeight().Return(uint64(1)).Times(2)

	cpuTarget, err := NewCPUTargeter(prometheus.NewRegistry(), &CPUTargeterConfig{CPUTarget: 2, VdrCPUPercentage: .5, MaxScaling: 20, SinglePeerMaxUsagePercentage: .5}, vdrs, cpuTracker)
	assert.NoError(t, err)
	startVdrTarget := cpuTarget.TargetCPUUsage(vdr1)
	startNonVdrTarget := cpuTarget.TargetCPUUsage(vdr2)

	cpuTracker.EXPECT().ActiveWeight().Return(uint64(10)).Times(2)
	assert.Greater(t, startVdrTarget, cpuTarget.TargetCPUUsage(vdr1))
	assert.Equal(t, startNonVdrTarget, cpuTarget.TargetCPUUsage(vdr2))
}

func TestCPUTargetLenScaling(t *testing.T) {
	vdr1 := ids.NodeID{1}
	vdr2 := ids.NodeID{2}
	vdrs := validators.NewSet()
	if err := vdrs.AddWeight(vdr1, 1); err != nil {
		t.Fatal(err)
	}

	ctrl := gomock.NewController(t)
	cpuTracker := NewMockTimeTracker(ctrl)
	cpuTracker.EXPECT().CumulativeUtilization(gomock.Any()).Return(2.0).AnyTimes()
	cpuTracker.EXPECT().Len().Return(1).Times(2)
	cpuTracker.EXPECT().ActiveWeight().Return(uint64(1)).AnyTimes()

	cpuTarget, err := NewCPUTargeter(prometheus.NewRegistry(), &CPUTargeterConfig{CPUTarget: 2, VdrCPUPercentage: .5, MaxScaling: 20, SinglePeerMaxUsagePercentage: .5}, vdrs, cpuTracker)
	assert.NoError(t, err)

	startVdrTarget := cpuTarget.TargetCPUUsage(vdr1)
	startNonVdrTarget := cpuTarget.TargetCPUUsage(vdr2)

	cpuTracker.EXPECT().Len().Return(10).Times(2)
	assert.Greater(t, startVdrTarget, cpuTarget.TargetCPUUsage(vdr1))
	assert.Greater(t, startNonVdrTarget, cpuTarget.TargetCPUUsage(vdr2))
}

func TestCPUTargetMaxPeerAmount(t *testing.T) {
	vdr1 := ids.NodeID{1}
	vdrs := validators.NewSet()
	ctrl := gomock.NewController(t)
	cpuTracker := NewMockTimeTracker(ctrl)
	cpuTracker.EXPECT().CumulativeUtilization(gomock.Any()).Return(2.0).AnyTimes()
	cpuTracker.EXPECT().Len().Return(1).AnyTimes()
	cpuTracker.EXPECT().ActiveWeight().Return(uint64(1)).AnyTimes()
	cpuTarget, err := NewCPUTargeter(prometheus.NewRegistry(), &CPUTargeterConfig{CPUTarget: 2, VdrCPUPercentage: .5, MaxScaling: 20, SinglePeerMaxUsagePercentage: .5}, vdrs, cpuTracker)
	assert.NoError(t, err)
	// would be 1 (CPUTarget * (1-VdrCPUPercentage)), but the SinglePeerMaxUsagePercentage limits it to half of that
	assert.Equal(t, 0.5, cpuTarget.TargetCPUUsage(vdr1))
}

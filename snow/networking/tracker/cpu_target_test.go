// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

// Assert fields are set correctly.
func TestNewCPUTargeter(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clock := mockable.Clock{}
	config := &CPUTargeterConfig{
		Clock:              mockable.Clock{},
		VdrCPUAlloc:        10,
		MaxNonVdrUsage:     10,
		MaxNonVdrNodeUsage: 10,
	}
	vdrs := validators.NewSet()
	cpuTracker := NewMockTimeTracker(ctrl)

	targeterIntf := NewCPUTargeter(
		config,
		vdrs,
		cpuTracker,
	)
	targeter, ok := targeterIntf.(*cpuTargeter)
	assert.True(ok)
	assert.Equal(clock, targeter.clock)
	assert.Equal(vdrs, targeter.vdrs)
	assert.Equal(cpuTracker, targeter.cpuTracker)
	assert.Equal(config.MaxNonVdrUsage, targeter.maxNonVdrUsage)
	assert.Equal(config.MaxNonVdrNodeUsage, targeter.maxNonVdrNodeUsage)
}

func TestCPUTarget(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	vdr := ids.NodeID{1}
	vdrWeight := uint64(1)
	totalVdrWeight := uint64(10)
	nonVdr := ids.NodeID{2}
	vdrs := validators.NewSet()
	if err := vdrs.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}
	if err := vdrs.AddWeight(ids.GenerateTestNodeID(), totalVdrWeight-vdrWeight); err != nil {
		t.Fatal(err)
	}

	cpuTracker := NewMockTimeTracker(ctrl)
	config := &CPUTargeterConfig{
		VdrCPUAlloc:        20,
		MaxNonVdrUsage:     10,
		MaxNonVdrNodeUsage: 5,
	}

	cpuTargeter := NewCPUTargeter(
		config,
		vdrs,
		cpuTracker,
	)

	type test struct {
		name             string
		setup            func()
		nodeID           ids.NodeID
		expectedCPUAlloc float64
	}
	tests := []test{
		{
			name: "Vdr alloc and at-large alloc",
			setup: func() {
				// At large utilization is less than max
				cpuTracker.EXPECT().CumulativeUtilization().Return(config.MaxNonVdrUsage - 1).Times(1)
			},
			nodeID:           vdr,
			expectedCPUAlloc: 2 + 1, // 20 * (1/10) + min(max(0,10-9),5)
		},
		{
			name: "no vdr alloc and at-large alloc",
			setup: func() {
				// At large utilization is less than max
				cpuTracker.EXPECT().CumulativeUtilization().Return(config.MaxNonVdrUsage - 1).Times(1)
			},
			nodeID:           nonVdr,
			expectedCPUAlloc: 0 + 1, // 0 * (1/10) + min(max(0,10-9), 5)
		},
		{
			name: "at-large alloc maxed",
			setup: func() {
				cpuTracker.EXPECT().CumulativeUtilization().Return(float64(0)).Times(1)
			},
			nodeID:           nonVdr,
			expectedCPUAlloc: 0 + 5, // 0 * (1/10) + min(max(0,10-0), 5)
		},
		{
			name: "at-large alloc completely used",
			setup: func() {
				cpuTracker.EXPECT().CumulativeUtilization().Return(config.MaxNonVdrUsage).Times(1)
			},
			nodeID:           nonVdr,
			expectedCPUAlloc: 0 + 0, // 0 * (1/10) + min(max(0,10-10), 5)
		},
		{
			name: "at-large alloc exceeded used",
			setup: func() {
				cpuTracker.EXPECT().CumulativeUtilization().Return(config.MaxNonVdrUsage + 1).Times(1)
			},
			nodeID:           nonVdr,
			expectedCPUAlloc: 0 + 0, // 0 * (1/10) + min(max(0,10-11), 5)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			cpuAlloc := cpuTargeter.TargetCPUUsage(tt.nodeID)
			assert.Equal(t, tt.expectedCPUAlloc, cpuAlloc)
		})
	}
}

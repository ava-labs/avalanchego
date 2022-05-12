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
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

// Assert fields are set correctly.
func TestNewCPUTargeter(t *testing.T) {
	assert := assert.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clock := mockable.Clock{}
	config := &CPUTargeterConfig{
		Clock:                 mockable.Clock{},
		VdrCPUAlloc:           10,
		AtLargeCPUAlloc:       11,
		PeerMaxAtLargePortion: 0.5,
	}
	vdrs := validators.NewSet()
	cpuTracker := NewMockTimeTracker(ctrl)

	targeterIntf, err := NewCPUTargeter(
		prometheus.NewRegistry(),
		config,
		vdrs,
		cpuTracker,
	)
	assert.NoError(err)
	targeter, ok := targeterIntf.(*cpuTargeter)
	assert.True(ok)
	assert.Equal(clock, targeter.clock)
	assert.Equal(vdrs, targeter.vdrs)
	assert.Equal(cpuTracker, targeter.cpuTracker)
	assert.Equal(config.VdrCPUAlloc, targeter.vdrCPUAlloc)
	assert.Equal(config.AtLargeCPUAlloc, targeter.atLargeCPUAlloc)
	assert.Equal(config.AtLargeCPUAlloc*config.PeerMaxAtLargePortion, targeter.atLargeMaxCPU)
	assert.NotNil(targeter.metrics)
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
		VdrCPUAlloc:           20,
		AtLargeCPUAlloc:       10,
		PeerMaxAtLargePortion: 0.5,
	}

	cpuTargeter, err := NewCPUTargeter(
		prometheus.NewRegistry(),
		config,
		vdrs,
		cpuTracker,
	)
	assert.NoError(t, err)

	type test struct {
		name                  string
		setup                 func()
		nodeID                ids.NodeID
		expectedVdrCPUAlloc   float64
		expectAtLargeCPUAlloc float64
	}
	tests := []test{
		{
			name: "Vdr alloc and at-large alloc",
			setup: func() {
				// At large utilization is less than max
				cpuTracker.EXPECT().CumulativeAtLargeUtilization(gomock.Any()).Return(config.AtLargeCPUAlloc - 1).Times(1)
			},
			nodeID:                vdr,
			expectedVdrCPUAlloc:   2, // 20 * (1/10)
			expectAtLargeCPUAlloc: 1, // min(max(0,10-9),5)
		},
		{
			name: "no vdr alloc and at-large alloc",
			setup: func() {
				// At large utilization is less than max
				cpuTracker.EXPECT().CumulativeAtLargeUtilization(gomock.Any()).Return(config.AtLargeCPUAlloc - 1).Times(1)
			},
			nodeID:                nonVdr,
			expectedVdrCPUAlloc:   0, // 0 * (1/10)
			expectAtLargeCPUAlloc: 1, // min(max(0,10-9), 5)
		},
		{
			name: "at-large alloc maxed",
			setup: func() {
				cpuTracker.EXPECT().CumulativeAtLargeUtilization(gomock.Any()).Return(float64(0)).Times(1)
			},
			nodeID:                nonVdr,
			expectedVdrCPUAlloc:   0, // 0 * (1/10)
			expectAtLargeCPUAlloc: 5, // min(max(0,10-0), 5)
		},
		{
			name: "at-large alloc completely used",
			setup: func() {
				cpuTracker.EXPECT().CumulativeAtLargeUtilization(gomock.Any()).Return(config.AtLargeCPUAlloc).Times(1)
			},
			nodeID:                nonVdr,
			expectedVdrCPUAlloc:   0, // 0 * (1/10)
			expectAtLargeCPUAlloc: 0, // min(max(0,10-10), 5)
		},
		{
			name: "at-large alloc exceeded used",
			setup: func() {
				cpuTracker.EXPECT().CumulativeAtLargeUtilization(gomock.Any()).Return(config.AtLargeCPUAlloc + 1).Times(1)
			},
			nodeID:                nonVdr,
			expectedVdrCPUAlloc:   0, // 0 * (1/10)
			expectAtLargeCPUAlloc: 0, // min(max(0,10-11), 5)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			gotVdrCPUAlloc, gotAtLargeCPUAlloc, _ := cpuTargeter.TargetCPUUsage(tt.nodeID)
			assert.Equal(t, tt.expectedVdrCPUAlloc, gotVdrCPUAlloc)
			assert.Equal(t, tt.expectAtLargeCPUAlloc, gotAtLargeCPUAlloc)
		})
	}
}

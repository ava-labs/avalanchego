// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

// Assert fields are set correctly.
func TestNewTargeter(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	config := &TargeterConfig{
		VdrAlloc:           10,
		MaxNonVdrUsage:     10,
		MaxNonVdrNodeUsage: 10,
	}
	vdrs := validators.NewSet()
	tracker := NewMockTracker(ctrl)

	targeterIntf := NewTargeter(
		config,
		vdrs,
		tracker,
	)
	targeter, ok := targeterIntf.(*targeter)
	require.True(ok)
	require.Equal(vdrs, targeter.vdrs)
	require.Equal(tracker, targeter.tracker)
	require.Equal(config.MaxNonVdrUsage, targeter.maxNonVdrUsage)
	require.Equal(config.MaxNonVdrNodeUsage, targeter.maxNonVdrNodeUsage)
}

func TestTarget(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	vdr := ids.NodeID{1}
	vdrWeight := uint64(1)
	totalVdrWeight := uint64(10)
	nonVdr := ids.NodeID{2}
	vdrs := validators.NewSet()
	if err := vdrs.Add(vdr, nil, ids.Empty, 1); err != nil {
		t.Fatal(err)
	}
	if err := vdrs.Add(ids.GenerateTestNodeID(), nil, ids.Empty, totalVdrWeight-vdrWeight); err != nil {
		t.Fatal(err)
	}

	tracker := NewMockTracker(ctrl)
	config := &TargeterConfig{
		VdrAlloc:           20,
		MaxNonVdrUsage:     10,
		MaxNonVdrNodeUsage: 5,
	}

	targeter := NewTargeter(
		config,
		vdrs,
		tracker,
	)

	type test struct {
		name           string
		setup          func()
		nodeID         ids.NodeID
		expectedTarget float64
	}
	tests := []test{
		{
			name: "Vdr alloc and at-large alloc",
			setup: func() {
				// At large utilization is less than max
				tracker.EXPECT().TotalUsage().Return(config.MaxNonVdrUsage - 1).Times(1)
			},
			nodeID:         vdr,
			expectedTarget: 2 + 1, // 20 * (1/10) + min(max(0,10-9),5)
		},
		{
			name: "no vdr alloc and at-large alloc",
			setup: func() {
				// At large utilization is less than max
				tracker.EXPECT().TotalUsage().Return(config.MaxNonVdrUsage - 1).Times(1)
			},
			nodeID:         nonVdr,
			expectedTarget: 0 + 1, // 0 * (1/10) + min(max(0,10-9), 5)
		},
		{
			name: "at-large alloc maxed",
			setup: func() {
				tracker.EXPECT().TotalUsage().Return(float64(0)).Times(1)
			},
			nodeID:         nonVdr,
			expectedTarget: 0 + 5, // 0 * (1/10) + min(max(0,10-0), 5)
		},
		{
			name: "at-large alloc completely used",
			setup: func() {
				tracker.EXPECT().TotalUsage().Return(config.MaxNonVdrUsage).Times(1)
			},
			nodeID:         nonVdr,
			expectedTarget: 0 + 0, // 0 * (1/10) + min(max(0,10-10), 5)
		},
		{
			name: "at-large alloc exceeded used",
			setup: func() {
				tracker.EXPECT().TotalUsage().Return(config.MaxNonVdrUsage + 1).Times(1)
			},
			nodeID:         nonVdr,
			expectedTarget: 0 + 0, // 0 * (1/10) + min(max(0,10-11), 5)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			target := targeter.TargetUsage(tt.nodeID)
			require.Equal(t, tt.expectedTarget, target)
		})
	}
}

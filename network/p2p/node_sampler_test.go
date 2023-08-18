// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestPeersSample(t *testing.T) {
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	nodeID3 := ids.GenerateTestNodeID()

	tests := []struct {
		name         string
		connected    set.Set[ids.NodeID]
		disconnected set.Set[ids.NodeID]
		n            int
	}{
		{
			name: "no peers",
			n:    1,
		},
		{
			name:      "one peer connected",
			connected: set.Of[ids.NodeID](nodeID1),
			n:         1,
		},
		{
			name:      "multiple peers connected",
			connected: set.Of[ids.NodeID](nodeID1, nodeID2, nodeID3),
			n:         1,
		},
		{
			name:         "peer connects and disconnects - 1",
			connected:    set.Of[ids.NodeID](nodeID1),
			disconnected: set.Of[ids.NodeID](nodeID1),
			n:            1,
		},
		{
			name:         "peer connects and disconnects - 2",
			connected:    set.Of[ids.NodeID](nodeID1, nodeID2),
			disconnected: set.Of[ids.NodeID](nodeID2),
			n:            1,
		},
		{
			name:         "peer connects and disconnects - 2",
			connected:    set.Of[ids.NodeID](nodeID1, nodeID2, nodeID3),
			disconnected: set.Of[ids.NodeID](nodeID1, nodeID2),
			n:            1,
		},
		{
			name:      "less than n peers",
			connected: set.Of[ids.NodeID](nodeID1, nodeID2, nodeID3),
			n:         4,
		},
		{
			name:      "n peers",
			connected: set.Of[ids.NodeID](nodeID1, nodeID2, nodeID3),
			n:         3,
		},
		{
			name:      "more than n peers",
			connected: set.Of[ids.NodeID](nodeID1, nodeID2, nodeID3),
			n:         2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			sampler := Peers{}

			for connected := range tt.connected {
				require.NoError(sampler.Connected(context.Background(), connected, nil))
			}

			for disconnected := range tt.disconnected {
				require.NoError(sampler.Disconnected(context.Background(), disconnected))
			}

			sampleable := set.Set[ids.NodeID]{}
			sampleable.Union(tt.connected)
			sampleable.Difference(tt.disconnected)

			sampled, err := sampler.Sample(tt.n)
			require.NoError(err)
			require.Len(sampled, safemath.Min(tt.n, len(sampleable)))
			require.Subset(sampleable, sampled)
		})
	}
}

func TestValidatorsSample(t *testing.T) {
	tests := []struct {
		name       string
		validators []ids.NodeID
		n          int
	}{
		{
			name:       "less than n validators",
			validators: []ids.NodeID{ids.GenerateTestNodeID()},
			n:          2,
		},
		{
			name: "equal to n validators",
			validators: []ids.NodeID{
				ids.GenerateTestNodeID(),
				ids.GenerateTestNodeID(),
			},
			n: 2,
		},
		{
			name: "greater than n validators",
			validators: []ids.NodeID{
				ids.GenerateTestNodeID(),
				ids.GenerateTestNodeID(),
				ids.GenerateTestNodeID(),
			},
			n: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			subnetID := ids.GenerateTestID()

			height := uint64(1234)
			mockValidators := validators.NewMockState(ctrl)
			mockValidators.EXPECT().GetCurrentHeight(gomock.Any()).Return(height, nil)

			validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, 0)
			for _, validator := range tt.validators {
				validatorSet[validator] = nil
			}
			mockValidators.EXPECT().GetValidatorSet(gomock.Any(), height, subnetID).Return(validatorSet, nil)

			v := Validators{
				subnetID:   subnetID,
				validators: mockValidators,
			}

			sampled, err := v.Sample(tt.n)
			require.NoError(err)
			require.Len(sampled, safemath.Min(tt.n, len(tt.validators)))
		})
	}
}

// invariant: we should only call GetValidatorSet when it passes a max staleness
// threshold
func TestValidatorsSampleCaching(t *testing.T) {
	tests := []struct {
		name          string
		validators    []ids.NodeID
		maxStaleness  time.Duration
		elapsed       time.Duration
		expectedCalls int
	}{
		{
			name:         "within max threshold",
			validators:   []ids.NodeID{ids.GenerateTestNodeID()},
			maxStaleness: time.Hour,
			elapsed:      time.Second,
		},
		{
			name:          "beyond max threshold",
			validators:    []ids.NodeID{ids.GenerateTestNodeID()},
			maxStaleness:  time.Hour,
			elapsed:       time.Hour + 1,
			expectedCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			subnetID := ids.GenerateTestID()

			height := uint64(1234)
			mockValidators := validators.NewMockState(ctrl)
			mockValidators.EXPECT().GetCurrentHeight(gomock.Any()).Return(height, nil).Times(tt.expectedCalls)

			validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, 0)
			for _, validator := range tt.validators {
				validatorSet[validator] = nil
			}
			mockValidators.EXPECT().GetValidatorSet(gomock.Any(), height, subnetID).Return(validatorSet, nil).Times(tt.expectedCalls)

			v := &Validators{
				subnetID:                 subnetID,
				validators:               mockValidators,
				maxValidatorSetStaleness: tt.maxStaleness,
			}
			v.lastUpdated = time.Now().Add(-tt.elapsed)

			_, err := v.Sample(1)
			require.NoError(err)
		})
	}
}

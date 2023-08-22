// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
)

// Sample should always return up to [limit] validators, and less if fewer than
// [limit] validators are available.
func TestValidatorsSample(t *testing.T) {
	tests := []struct {
		name  string
		limit int

		currentHeight       uint64
		getCurrentHeightErr error

		validatorSet       map[ids.NodeID]*validators.GetValidatorOutput
		getValidatorSetErr error
	}{
		{
			name:  "less than limit validators",
			limit: 2,
			validatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				ids.GenerateTestNodeID(): nil,
			},
		},
		{
			name:  "equal to limit validators",
			limit: 2,
			validatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				ids.GenerateTestNodeID(): nil,
				ids.GenerateTestNodeID(): nil,
			},
		},
		{
			name:  "greater than limit validators",
			limit: 2,
			validatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				ids.GenerateTestNodeID(): nil,
				ids.GenerateTestNodeID(): nil,
				ids.GenerateTestNodeID(): nil,
			},
		},
		{
			name:                "fail to get current height",
			limit:               2,
			getCurrentHeightErr: errors.New("foobar"),
		},
		{
			name:                "fail to get validator set",
			limit:               2,
			getCurrentHeightErr: errors.New("foobar"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			subnetID := ids.GenerateTestID()

			mockValidators := validators.NewMockState(ctrl)
			mockValidators.EXPECT().GetCurrentHeight(gomock.Any()).Return(tt.currentHeight, tt.getCurrentHeightErr)

			getValidatorSetExpectedCalls := 0
			if tt.getCurrentHeightErr == nil {
				getValidatorSetExpectedCalls = 1
			}
			mockValidators.EXPECT().GetValidatorSet(gomock.Any(), tt.currentHeight, subnetID).Return(tt.validatorSet, tt.getValidatorSetErr).Times(getValidatorSetExpectedCalls)

			v := NewValidators(subnetID, mockValidators)

			sampled := v.Sample(context.Background(), tt.limit)
			require.Len(sampled, math.Min(tt.limit, len(tt.validatorSet)))
			require.Subset(maps.Keys(tt.validatorSet), sampled)
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

			v := NewValidators(subnetID, mockValidators)
			v.maxValidatorSetStaleness = tt.maxStaleness
			v.lastUpdated = time.Now().Add(-tt.elapsed)

			v.Sample(context.Background(), 1)
		})
	}
}

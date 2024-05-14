// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestValidatorsTop(t *testing.T) {
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	nodeID3 := ids.GenerateTestNodeID()

	tests := []struct {
		name       string
		validators []validator
		percentage float64
		expected   []ids.NodeID
	}{
		{
			name: "top 0% is empty",
			validators: []validator{
				{
					nodeID: nodeID1,
					weight: 1,
				},
				{
					nodeID: nodeID2,
					weight: 1,
				},
			},
			percentage: 0,
			expected:   []ids.NodeID{},
		},
		{
			name: "top 100% is full",
			validators: []validator{
				{
					nodeID: nodeID1,
					weight: 2,
				},
				{
					nodeID: nodeID2,
					weight: 1,
				},
			},
			percentage: 1,
			expected: []ids.NodeID{
				nodeID1,
				nodeID2,
			},
		},
		{
			name: "top 50% takes larger validator",
			validators: []validator{
				{
					nodeID: nodeID1,
					weight: 2,
				},
				{
					nodeID: nodeID2,
					weight: 1,
				},
			},
			percentage: .5,
			expected: []ids.NodeID{
				nodeID1,
			},
		},
		{
			name: "top 50% bound",
			validators: []validator{
				{
					nodeID: nodeID1,
					weight: 2,
				},
				{
					nodeID: nodeID2,
					weight: 1,
				},
				{
					nodeID: nodeID3,
					weight: 1,
				},
			},
			percentage: .5,
			expected: []ids.NodeID{
				nodeID1,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, 0)
			for _, validator := range test.validators {
				validatorSet[validator.nodeID] = &validators.GetValidatorOutput{
					NodeID: validator.nodeID,
					Weight: validator.weight,
				}
			}

			subnetID := ids.GenerateTestID()
			mockValidators := validators.NewMockState(ctrl)

			mockValidators.EXPECT().GetCurrentHeight(gomock.Any()).Return(uint64(1), nil)
			mockValidators.EXPECT().GetValidatorSet(gomock.Any(), uint64(1), subnetID).Return(validatorSet, nil)

			v := newValidators(logging.NoLog{}, subnetID, mockValidators, time.Second)
			ctx := context.Background()
			nodeIDs := v.Top(ctx, test.percentage)
			require.Equal(test.expected, nodeIDs)
		})
	}
}

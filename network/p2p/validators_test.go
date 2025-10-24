// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestValidatorsSample(t *testing.T) {
	errFoobar := errors.New("foobar")
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	nodeID3 := ids.GenerateTestNodeID()

	type call struct {
		limit int

		time time.Time

		height              uint64
		getCurrentHeightErr error

		validators         []ids.NodeID
		getValidatorSetErr error

		// superset of possible values in the result
		expected []ids.NodeID
	}

	tests := []struct {
		name         string
		maxStaleness time.Duration
		calls        []call
	}{
		{
			// if we aren't connected to a validator, we shouldn't return it
			name:         "drop disconnected validators",
			maxStaleness: time.Hour,
			calls: []call{
				{
					time:       time.Time{}.Add(time.Second),
					limit:      2,
					height:     1,
					validators: []ids.NodeID{nodeID1, nodeID3},
					expected:   []ids.NodeID{nodeID1},
				},
			},
		},
		{
			// if we don't have as many validators as requested by the caller,
			// we should return all the validators we have
			name:         "less than limit validators",
			maxStaleness: time.Hour,
			calls: []call{
				{
					time:       time.Time{}.Add(time.Second),
					limit:      2,
					height:     1,
					validators: []ids.NodeID{nodeID1},
					expected:   []ids.NodeID{nodeID1},
				},
			},
		},
		{
			// if we have as many validators as requested by the caller, we
			// should return all the validators we have
			name:         "equal to limit validators",
			maxStaleness: time.Hour,
			calls: []call{
				{
					time:       time.Time{}.Add(time.Second),
					limit:      1,
					height:     1,
					validators: []ids.NodeID{nodeID1},
					expected:   []ids.NodeID{nodeID1},
				},
			},
		},
		{
			// if we have less validators than requested by the caller, we
			// should return a subset of the validators that we have
			name:         "less than limit validators",
			maxStaleness: time.Hour,
			calls: []call{
				{
					time:       time.Time{}.Add(time.Second),
					limit:      1,
					height:     1,
					validators: []ids.NodeID{nodeID1, nodeID2},
					expected:   []ids.NodeID{nodeID1, nodeID2},
				},
			},
		},
		{
			name:         "within max staleness threshold",
			maxStaleness: time.Hour,
			calls: []call{
				{
					time:       time.Time{}.Add(time.Second),
					limit:      1,
					height:     1,
					validators: []ids.NodeID{nodeID1},
					expected:   []ids.NodeID{nodeID1},
				},
			},
		},
		{
			name:         "beyond max staleness threshold",
			maxStaleness: time.Hour,
			calls: []call{
				{
					limit:      1,
					time:       time.Time{}.Add(time.Hour),
					height:     1,
					validators: []ids.NodeID{nodeID1},
					expected:   []ids.NodeID{nodeID1},
				},
			},
		},
		{
			name:         "fail to get current height",
			maxStaleness: time.Second,
			calls: []call{
				{
					limit:               1,
					time:                time.Time{}.Add(time.Hour),
					getCurrentHeightErr: errFoobar,
					expected:            []ids.NodeID{},
				},
			},
		},
		{
			name:         "second get validator set call fails",
			maxStaleness: time.Minute,
			calls: []call{
				{
					limit:      1,
					time:       time.Time{}.Add(time.Second),
					height:     1,
					validators: []ids.NodeID{nodeID1},
					expected:   []ids.NodeID{nodeID1},
				},
				{
					limit:              1,
					time:               time.Time{}.Add(time.Hour),
					height:             1,
					getValidatorSetErr: errFoobar,
					expected:           []ids.NodeID{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			subnetID := ids.GenerateTestID()
			ctrl := gomock.NewController(t)
			mockValidators := validatorsmock.NewState(ctrl)

			calls := make([]any, 0, 2*len(tt.calls))
			for _, call := range tt.calls {
				calls = append(
					calls,
					mockValidators.EXPECT().GetCurrentHeight(gomock.Any()).Return(call.height, call.getCurrentHeightErr),
				)

				if call.getCurrentHeightErr != nil {
					continue
				}

				validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, len(call.validators))
				for _, validator := range call.validators {
					validatorSet[validator] = &validators.GetValidatorOutput{
						NodeID: validator,
						Weight: 1,
					}
				}

				calls = append(
					calls,
					mockValidators.EXPECT().GetValidatorSet(gomock.Any(), gomock.Any(), subnetID).Return(validatorSet, call.getValidatorSetErr),
				)
			}
			gomock.InOrder(calls...)

			network, err := NewNetwork(
				logging.NoLog{},
				&enginetest.SenderStub{},
				prometheus.NewRegistry(),
				"",
			)
			require.NoError(err)

			ctx := context.Background()
			require.NoError(network.Connected(ctx, nodeID1, nil))
			require.NoError(network.Connected(ctx, nodeID2, nil))

			v := NewValidators(
				logging.NoLog{},
				subnetID,
				mockValidators,
				tt.maxStaleness,
			)
			for _, call := range tt.calls {
				v.lastUpdated = call.time
				sampled := v.Sample(ctx, call.limit)
				require.LessOrEqual(len(sampled), call.limit)
				require.Subset(call.expected, sampled)
			}
		})
	}
}

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
		{
			name: "top ignores inactive validators",
			validators: []validator{
				{
					nodeID: ids.EmptyNodeID,
					weight: 4,
				},
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

			validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, len(test.validators))
			for _, validator := range test.validators {
				validatorSet[validator.nodeID] = &validators.GetValidatorOutput{
					NodeID: validator.nodeID,
					Weight: validator.weight,
				}
			}

			subnetID := ids.GenerateTestID()
			mockValidators := validatorsmock.NewState(ctrl)

			mockValidators.EXPECT().GetCurrentHeight(gomock.Any()).Return(uint64(1), nil)
			mockValidators.EXPECT().GetValidatorSet(gomock.Any(), uint64(1), subnetID).Return(validatorSet, nil)

			network, err := NewNetwork(
				logging.NoLog{},
				&enginetest.SenderStub{},
				prometheus.NewRegistry(),
				"",
			)
			require.NoError(err)

			ctx := context.Background()
			require.NoError(network.Connected(ctx, nodeID1, nil))
			require.NoError(network.Connected(ctx, nodeID2, nil))

			v := NewValidators(logging.NoLog{}, subnetID, mockValidators, time.Second)
			nodeIDs := v.Top(ctx, test.percentage)
			require.Equal(test.expected, nodeIDs)
		})
	}
}

// TestValidatorsLock tests that [validators.State] is not accessed with the
// [Validators] lock held.
func TestValidatorsLock(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockValidators := validatorsmock.NewState(ctrl)
	subnetID := ids.GenerateTestID()

	var v *Validators
	mockValidators.EXPECT().GetCurrentHeight(gomock.Any()).DoAndReturn(func(context.Context) (uint64, error) {
		// Assert that the validators lock is not held during calls to
		// GetCurrentHeight.
		require.True(t, v.lock.TryLock())
		v.lock.Unlock()
		return 1, nil
	})
	mockValidators.EXPECT().GetValidatorSet(gomock.Any(), uint64(1), subnetID).Return(nil, nil)

	v = NewValidators(logging.NoLog{}, subnetID, mockValidators, time.Second)
	_ = v.Len(context.Background())
}

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

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
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

			validatorState := &validatorstest.State{}
			network, err := NewNetwork(
				logging.NoLog{},
				&enginetest.SenderStub{},
				validatorState,
				subnetID,
				time.Second,
				prometheus.NewRegistry(),
				"",
			)
			require.NoError(err)

			ctx := context.Background()
			require.NoError(network.Connected(ctx, nodeID1, nil))
			require.NoError(network.Connected(ctx, nodeID2, nil))

			for _, call := range tt.calls {
				network.Validators.lastUpdated = call.time
				validatorState.GetCurrentHeightF = func(context.Context) (
					uint64,
					error,
				) {
					return call.height, call.getCurrentHeightErr
				}
				validatorState.GetValidatorSetF = func(
					context.Context,
					uint64,
					ids.ID,
				) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
					validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput)

					for _, nodeID := range call.validators {
						validatorSet[nodeID] = &validators.GetValidatorOutput{
							NodeID: nodeID,
							Weight: 1,
						}
					}

					return validatorSet, call.getValidatorSetErr
				}

				sampled := network.Validators.Sample(ctx, call.limit)
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

			validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, 0)
			for _, validator := range test.validators {
				validatorSet[validator.nodeID] = &validators.GetValidatorOutput{
					NodeID: validator.nodeID,
					Weight: validator.weight,
				}
			}

			network, err := NewNetwork(
				logging.NoLog{},
				&enginetest.SenderStub{},
				&validatorstest.State{
					GetCurrentHeightF: func(context.Context) (uint64, error) {
						return 0, nil
					},
					GetValidatorSetF: func(
						context.Context,
						uint64,
						ids.ID,
					) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return validatorSet, nil
					},
				},
				ids.Empty,
				time.Second,
				prometheus.NewRegistry(),
				"",
			)
			require.NoError(err)

			ctx := context.Background()
			require.NoError(network.Connected(ctx, nodeID1, nil))
			require.NoError(network.Connected(ctx, nodeID2, nil))

			nodeIDs := network.Validators.Top(ctx, test.percentage)
			require.Equal(test.expected, nodeIDs)
		})
	}
}

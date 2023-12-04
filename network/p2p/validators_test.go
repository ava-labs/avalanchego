// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestValidatorsSample(t *testing.T) {
	errFoobar := errors.New("foobar")
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()

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
			mockValidators := validators.NewMockState(ctrl)

			calls := make([]*gomock.Call, 0)
			for _, call := range tt.calls {
				calls = append(calls, mockValidators.EXPECT().
					GetCurrentHeight(gomock.Any()).Return(call.height, call.getCurrentHeightErr))

				if call.getCurrentHeightErr != nil {
					continue
				}

				validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, 0)
				for _, validator := range call.validators {
					validatorSet[validator] = nil
				}

				calls = append(calls,
					mockValidators.EXPECT().
						GetValidatorSet(gomock.Any(), gomock.Any(), subnetID).
						Return(validatorSet, call.getValidatorSetErr))
			}
			gomock.InOrder(calls...)

			network := NewNetwork(logging.NoLog{}, &common.SenderTest{}, prometheus.NewRegistry(), "")
			ctx := context.Background()
			require.NoError(network.Connected(ctx, nodeID1, nil))
			require.NoError(network.Connected(ctx, nodeID2, nil))

			v := NewValidators(network.Peers, network.log, subnetID, mockValidators, tt.maxStaleness)
			for _, call := range tt.calls {
				v.lastUpdated = call.time
				sampled := v.Sample(context.Background(), call.limit)
				require.LessOrEqual(len(sampled), call.limit)
				require.Subset(call.expected, sampled)
			}
		})
	}
}

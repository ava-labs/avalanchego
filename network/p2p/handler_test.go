// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ ValidatorSet = (*testValidatorSet)(nil)

type testValidatorSet struct {
	validators set.Set[ids.NodeID]
}

func (t testValidatorSet) Len(context.Context) int {
	return len(t.validators)
}

func (t testValidatorSet) Has(_ context.Context, nodeID ids.NodeID) bool {
	return t.validators.Contains(nodeID)
}

func TestValidatorHandlerAppGossip(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	validatorSet := set.Of(nodeID)

	tests := []struct {
		name         string
		validatorSet ValidatorSet
		nodeID       ids.NodeID
		expected     bool
	}{
		{
			name:         "message dropped",
			validatorSet: testValidatorSet{},
			nodeID:       nodeID,
		},
		{
			name: "message handled",
			validatorSet: testValidatorSet{
				validators: validatorSet,
			},
			nodeID:   nodeID,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			called := false
			handler := NewValidatorHandler(
				&TestHandler{
					AppGossipF: func(context.Context, ids.NodeID, []byte) {
						called = true
					},
				},
				tt.validatorSet,
				logging.NoLog{},
			)

			handler.AppGossip(t.Context(), tt.nodeID, []byte("foobar"))
			require.Equal(tt.expected, called)
		})
	}
}

func TestValidatorHandlerAppRequest(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	validatorSet := set.Of(nodeID)

	tests := []struct {
		name         string
		validatorSet ValidatorSet
		nodeID       ids.NodeID
		expected     *common.AppError
	}{
		{
			name:         "message dropped",
			validatorSet: testValidatorSet{},
			nodeID:       nodeID,
			expected:     ErrNotValidator,
		},
		{
			name: "message handled",
			validatorSet: testValidatorSet{
				validators: validatorSet,
			},
			nodeID: nodeID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			handler := NewValidatorHandler(
				NoOpHandler{},
				tt.validatorSet,
				logging.NoLog{},
			)

			_, err := handler.AppRequest(t.Context(), tt.nodeID, time.Time{}, []byte("foobar"))
			require.ErrorIs(err, tt.expected)
		})
	}
}

func TestNewDynamicThrottlerHandler_AppRequest(t *testing.T) {
	type request struct {
		nodeID  ids.NodeID
		wantErr *common.AppError
	}

	tests := []struct {
		name             string
		throttlingPeriod time.Duration
		requestsPerPeer  float64

		// must be same length
		validatorSets [][]ids.NodeID
		requests      [][]request
	}{
		{
			name:             "no validators",
			throttlingPeriod: time.Hour,
			requestsPerPeer:  1,
			validatorSets: [][]ids.NodeID{
				{},
			},
			requests: [][]request{
				{
					{nodeID: ids.NodeID{1}, wantErr: ErrThrottled},
				},
			},
		},
		{
			name:             "all validators un-stake",
			throttlingPeriod: time.Hour,
			requestsPerPeer:  1,
			validatorSets: [][]ids.NodeID{
				{{1}},
				{},
			},
			requests: [][]request{
				{
					{nodeID: ids.NodeID{1}},
				},
				{
					{nodeID: ids.NodeID{1}, wantErr: ErrThrottled},
				},
			},
		},
		{
			name:             "validator request",
			throttlingPeriod: time.Hour,
			requestsPerPeer:  1,
			validatorSets: [][]ids.NodeID{
				{
					{1},
				},
			},
			requests: [][]request{
				{
					{nodeID: ids.NodeID{1}},
				},
			},
		},
		{
			name:             "validator requests not throttled",
			throttlingPeriod: time.Hour,
			requestsPerPeer:  5,
			validatorSets: [][]ids.NodeID{
				{
					{1},
				},
			},
			requests: [][]request{
				{
					{nodeID: ids.NodeID{1}},
					{nodeID: ids.NodeID{1}},
					{nodeID: ids.NodeID{1}},
				},
			},
		},
		{
			name:             "validator request throttled",
			throttlingPeriod: time.Hour,
			requestsPerPeer:  3,
			validatorSets: [][]ids.NodeID{
				{
					{1},
				},
			},
			requests: [][]request{
				{
					{nodeID: ids.NodeID{1}},
					{nodeID: ids.NodeID{1}},
					{nodeID: ids.NodeID{1}},
					{nodeID: ids.NodeID{1}, wantErr: ErrThrottled},
				},
			},
		},
		{
			name:             "validator request throttled before update",
			throttlingPeriod: time.Hour,
			requestsPerPeer:  3,
			validatorSets: [][]ids.NodeID{
				{
					{1},
				},
				{
					{1}, {2}, {3}, {4}, {5}, {6}, {7}, {8}, {9},
				},
			},
			requests: [][]request{
				{
					{nodeID: ids.NodeID{1}},
					{nodeID: ids.NodeID{1}},
					{nodeID: ids.NodeID{1}},
				},
				{
					{nodeID: ids.NodeID{1}, wantErr: ErrThrottled},
				},
			},
		},
		{
			name:             "validator request throttled after update",
			throttlingPeriod: time.Hour,
			requestsPerPeer:  3,
			validatorSets: [][]ids.NodeID{
				{
					{1},
				},
				{
					{1},
					{2},
					{3},
					{4},
					{5},
					{6},
					{7},
					{8},
					{9},
					{10},
					{11},
					{12},
					{13},
					{14},
					{15},
					{16},
					{17},
					{18},
					{19},
					{20},
				},
			},
			requests: [][]request{
				{
					{nodeID: ids.NodeID{1}},
				},
				{
					{nodeID: ids.NodeID{1}},
					{nodeID: ids.NodeID{1}, wantErr: ErrThrottled},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.Len(tt.validatorSets, len(tt.requests))

			validatorSet := &testValidatorSet{}
			handler, err := NewDynamicThrottlerHandler(
				logging.NoLog{},
				NoOpHandler{},
				validatorSet,
				tt.throttlingPeriod,
				tt.requestsPerPeer,
				prometheus.NewRegistry(),
				"",
			)
			require.NoError(err)

			for i, latest := range tt.validatorSets {
				validatorSet.validators = set.Of(latest...)

				for _, r := range tt.requests[i] {
					_, err := handler.AppRequest(
						t.Context(),
						r.nodeID,
						time.Time{},
						[]byte("foobar"),
					)
					require.ErrorIs(err, r.wantErr)
				}

				// The throttling limit should be at least the number of expected samples
				wantSamples := float64(0)
				if len(latest) > 0 {
					wantSamples = tt.requestsPerPeer / float64(len(latest))
				}

				require.GreaterOrEqual(
					testutil.ToFloat64(handler.throttleLimitMetric),
					wantSamples,
				)
			}
		})
	}
}

func TestNewDynamicThrottlerHandler(t *testing.T) {
	tests := []struct {
		name             string
		throttlingPeriod time.Duration
		requestsPerPeer  float64
		wantErr          error
	}{
		{
			name:             "negative throttling period",
			throttlingPeriod: -1,
			requestsPerPeer:  1,
			wantErr:          errPeriodMustBePositive,
		},
		{
			name:            "zero throttling period",
			requestsPerPeer: 1,
			wantErr:         errPeriodMustBePositive,
		},
		{
			name:             "positive throttling period",
			throttlingPeriod: time.Second,
			requestsPerPeer:  1,
		},
		{
			name:             "NaN requests-per-peer",
			throttlingPeriod: time.Second,
			requestsPerPeer:  math.NaN(),
			wantErr:          errRequestsPerPeerMustBeNonNegative,
		},
		{
			name:             "negative requests-per-peer",
			throttlingPeriod: time.Second,
			requestsPerPeer:  -1,
			wantErr:          errRequestsPerPeerMustBeNonNegative,
		},
		{
			name:             "zero requests-per-peer",
			throttlingPeriod: time.Second,
		},
		{
			name:             "positive requests-per-peer",
			throttlingPeriod: time.Second,
			requestsPerPeer:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			_, err := NewDynamicThrottlerHandler(
				logging.NoLog{},
				NoOpHandler{},
				&testValidatorSet{},
				tt.throttlingPeriod,
				tt.requestsPerPeer,
				prometheus.NewRegistry(),
				"",
			)
			require.ErrorIs(err, tt.wantErr)
		})
	}
}

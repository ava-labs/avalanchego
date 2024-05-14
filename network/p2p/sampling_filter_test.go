// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestPeerSamplingFilter(t *testing.T) {
	tests := []struct {
		name     string
		self     ids.NodeID
		nodeID   ids.NodeID
		expected bool
	}{
		{
			name:   "dropped from filter",
			self:   ids.EmptyNodeID,
			nodeID: ids.EmptyNodeID,
		},
		{
			name:     "in filter",
			self:     ids.EmptyNodeID,
			nodeID:   ids.GenerateTestNodeID(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			filter := NewPeerSamplingFilter(tt.self)

			require.Equal(
				tt.expected,
				filter.Filter(context.Background(), tt.nodeID),
			)
		})
	}
}

func TestValidatorSamplingFilter(t *testing.T) {
	tests := []struct {
		name       string
		validators []ids.NodeID
		nodeID     ids.NodeID
		expected   bool
	}{
		{
			name:       "dropped from filter",
			validators: []ids.NodeID{ids.GenerateTestNodeID()},
			nodeID:     ids.GenerateTestNodeID(),
		},
		{
			name:       "in filter",
			validators: []ids.NodeID{ids.EmptyNodeID},
			nodeID:     ids.EmptyNodeID,
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			validatorSet := set.Set[ids.NodeID]{}
			for _, validator := range tt.validators {
				validatorSet.Add(validator)
			}

			filter := NewValidatorSamplingFilter(
				&testValidatorSet{
					validators: validatorSet,
				},
			)

			require.Equal(
				tt.expected,
				filter.Filter(context.Background(), tt.nodeID),
			)
		})
	}
}

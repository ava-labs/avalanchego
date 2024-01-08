// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func newTestIPTracker(t *testing.T) *ipTracker {
	tracker, err := newIPTracker(logging.NoLog{})
	require.NoError(t, err)
	return tracker
}

func requireEqual(t *testing.T, expected, actual *ipTracker) {
	require := require.New(t)
	require.Equal(expected.manuallyTracked, actual.manuallyTracked)
	require.Equal(expected.connected, actual.connected)
	require.Equal(expected.mostRecentValidatorIPs, actual.mostRecentValidatorIPs)
	require.Equal(expected.validators, actual.validators)
	require.Equal(expected.gossipableIndicies, actual.gossipableIndicies)
	require.Equal(expected.gossipableIPs, actual.gossipableIPs)
	require.Equal(expected.bloomAdditions, actual.bloomAdditions)
	require.Equal(expected.maxBloomCount, actual.maxBloomCount)
}

func TestIPTracker_ManuallyTrack(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	tests := []struct {
		name          string
		initialState  *ipTracker
		nodeID        ids.NodeID
		expectedState *ipTracker
	}{
		{
			name:         "non-connected non-validator",
			initialState: newTestIPTracker(t),
			nodeID:       nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.manuallyTracked.Add(nodeID)
				tracker.validators.Add(nodeID)
				return tracker
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.initialState.ManuallyTrack(test.nodeID)
			requireEqual(t, test.expectedState, test.initialState)
		})
	}
}

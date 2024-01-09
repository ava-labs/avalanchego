// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/ips"
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
	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)
	cert := staking.CertificateFromX509(tlsCert.Leaf)

	ip := &ips.ClaimedIPPort{
		Cert: cert,
		IPPort: ips.IPPort{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 9651,
		},
		Timestamp: 1,
	}
	nodeID := ip.NodeID()
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
				tracker.validators.Add(nodeID)
				tracker.manuallyTracked.Add(nodeID)
				return tracker
			}(),
		},
		{
			name: "connected non-validator",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.mostRecentValidatorIPs[nodeID] = ip
				tracker.bloomAdditions[nodeID] = 1
				tracker.gossipableIndicies[nodeID] = 0
				tracker.gossipableIPs = []*ips.ClaimedIPPort{
					ip,
				}
				tracker.validators.Add(nodeID)
				tracker.manuallyTracked.Add(nodeID)
				return tracker
			}(),
		},
		{
			name: "non-connected validator",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				return tracker
			}(),
			nodeID: nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.manuallyTracked.Add(nodeID)
				return tracker
			}(),
		},
		{
			name: "connected validator",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.onValidatorAdded(nodeID)
				return tracker
			}(),
			nodeID: nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.onValidatorAdded(nodeID)
				tracker.manuallyTracked.Add(nodeID)
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

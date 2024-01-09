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

func newTestIP(t *testing.T) *ips.ClaimedIPPort {
	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)
	cert := staking.CertificateFromX509(tlsCert.Leaf)

	return &ips.ClaimedIPPort{
		Cert: cert,
		IPPort: ips.IPPort{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 9651,
		},
		Timestamp: 1,
	}
}

func newerTestIP(ip *ips.ClaimedIPPort) *ips.ClaimedIPPort {
	return &ips.ClaimedIPPort{
		Cert:      ip.Cert,
		IPPort:    ip.IPPort,
		Timestamp: ip.Timestamp + 1,
	}
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
	ip := newTestIP(t)
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

func TestIPTracker_AddIP(t *testing.T) {
	ip := newTestIP(t)
	newerIP := newerTestIP(ip)
	nodeID := ip.NodeID()
	tests := []struct {
		name            string
		initialState    *ipTracker
		ip              *ips.ClaimedIPPort
		expectedUpdated bool
		expectedState   *ipTracker
	}{
		{
			name:            "non-validator",
			initialState:    newTestIPTracker(t),
			ip:              ip,
			expectedUpdated: false,
			expectedState:   newTestIPTracker(t),
		},
		{
			name: "first known IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				return tracker
			}(),
			ip:              ip,
			expectedUpdated: true,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.mostRecentValidatorIPs[nodeID] = ip
				tracker.bloomAdditions[nodeID] = 1
				return tracker
			}(),
		},
		{
			name: "older IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			}(),
			ip:              ip,
			expectedUpdated: false,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			}(),
		},
		{
			name: "same IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip:              ip,
			expectedUpdated: false,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
		},
		{
			name: "disconnected newer IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip:              newerIP,
			expectedUpdated: true,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(ip))
				tracker.mostRecentValidatorIPs[nodeID] = newerIP
				tracker.bloomAdditions[nodeID] = 2
				return tracker
			}(),
		},
		{
			name: "connected newer IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				return tracker
			}(),
			ip:              newerIP,
			expectedUpdated: true,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				tracker.mostRecentValidatorIPs[nodeID] = newerIP
				tracker.bloomAdditions[nodeID] = 2
				delete(tracker.gossipableIndicies, nodeID)
				tracker.gossipableIPs = tracker.gossipableIPs[:0]
				return tracker
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updated := test.initialState.AddIP(test.ip)
			require.Equal(t, test.expectedUpdated, updated)
			requireEqual(t, test.expectedState, test.initialState)
		})
	}
}

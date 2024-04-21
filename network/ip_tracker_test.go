// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func newTestIPTracker(t *testing.T) *ipTracker {
	tracker, err := newIPTracker(logging.NoLog{}, "", prometheus.NewRegistry())
	require.NoError(t, err)
	return tracker
}

func newerTestIP(ip *ips.ClaimedIPPort) *ips.ClaimedIPPort {
	return ips.NewClaimedIPPort(
		ip.Cert,
		ip.IPPort,
		ip.Timestamp+1,
		ip.Signature,
	)
}

func requireEqual(t *testing.T, expected, actual *ipTracker) {
	require := require.New(t)
	require.Equal(expected.manuallyTracked, actual.manuallyTracked)
	require.Equal(expected.manuallyGossipable, actual.manuallyGossipable)
	require.Equal(expected.mostRecentTrackedIPs, actual.mostRecentTrackedIPs)
	require.Equal(expected.trackedIDs, actual.trackedIDs)
	require.Equal(expected.bloomAdditions, actual.bloomAdditions)
	require.Equal(expected.maxBloomCount, actual.maxBloomCount)
	require.Equal(expected.connected, actual.connected)
	require.Equal(expected.gossipableIndices, actual.gossipableIndices)
	require.Equal(expected.gossipableIPs, actual.gossipableIPs)
	require.Equal(expected.gossipableIDs, actual.gossipableIDs)
}

func requireMetricsConsistent(t *testing.T, tracker *ipTracker) {
	require := require.New(t)
	require.Equal(float64(len(tracker.mostRecentTrackedIPs)), testutil.ToFloat64(tracker.numTrackedIPs))
	require.Equal(float64(len(tracker.gossipableIPs)), testutil.ToFloat64(tracker.numGossipableIPs))
	require.Equal(float64(tracker.bloom.Count()), testutil.ToFloat64(tracker.bloomMetrics.Count))
	require.Equal(float64(tracker.maxBloomCount), testutil.ToFloat64(tracker.bloomMetrics.MaxCount))
}

func TestIPTracker_ManuallyTrack(t *testing.T) {
	tests := []struct {
		name          string
		initialState  *ipTracker
		nodeID        ids.NodeID
		expectedState *ipTracker
	}{
		{
			name:         "non-connected non-validator",
			initialState: newTestIPTracker(t),
			nodeID:       ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.manuallyTracked.Add(ip.NodeID)
				tracker.trackedIDs.Add(ip.NodeID)
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
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.manuallyTracked.Add(ip.NodeID)
				tracker.mostRecentTrackedIPs[ip.NodeID] = ip
				tracker.trackedIDs.Add(ip.NodeID)
				tracker.bloomAdditions[ip.NodeID] = 1
				return tracker
			}(),
		},
		{
			name: "non-connected validator",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.manuallyTracked.Add(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "connected validator",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.manuallyTracked.Add(ip.NodeID)
				return tracker
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.initialState.ManuallyTrack(test.nodeID)
			requireEqual(t, test.expectedState, test.initialState)
			requireMetricsConsistent(t, test.initialState)
		})
	}
}

func TestIPTracker_ManuallyGossip(t *testing.T) {
	tests := []struct {
		name          string
		initialState  *ipTracker
		nodeID        ids.NodeID
		expectedState *ipTracker
	}{
		{
			name:         "non-connected non-validator",
			initialState: newTestIPTracker(t),
			nodeID:       ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.manuallyTracked.Add(ip.NodeID)
				tracker.manuallyGossipable.Add(ip.NodeID)
				tracker.trackedIDs.Add(ip.NodeID)
				tracker.gossipableIDs.Add(ip.NodeID)
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
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.manuallyTracked.Add(ip.NodeID)
				tracker.manuallyGossipable.Add(ip.NodeID)
				tracker.mostRecentTrackedIPs[ip.NodeID] = ip
				tracker.trackedIDs.Add(ip.NodeID)
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.gossipableIndices[ip.NodeID] = 0
				tracker.gossipableIPs = []*ips.ClaimedIPPort{
					ip,
				}
				tracker.gossipableIDs.Add(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "non-connected validator",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.manuallyTracked.Add(ip.NodeID)
				tracker.manuallyGossipable.Add(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "connected validator",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.manuallyTracked.Add(ip.NodeID)
				tracker.manuallyGossipable.Add(ip.NodeID)
				return tracker
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.initialState.ManuallyGossip(test.nodeID)
			requireEqual(t, test.expectedState, test.initialState)
			requireMetricsConsistent(t, test.initialState)
		})
	}
}

func TestIPTracker_AddIP(t *testing.T) {
	newerIP := newerTestIP(ip)
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
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				return tracker
			}(),
			ip:              ip,
			expectedUpdated: true,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.mostRecentTrackedIPs[ip.NodeID] = ip
				tracker.bloomAdditions[ip.NodeID] = 1
				return tracker
			}(),
		},
		{
			name: "older IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			}(),
			ip:              ip,
			expectedUpdated: false,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			}(),
		},
		{
			name: "same IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip:              ip,
			expectedUpdated: false,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
		},
		{
			name: "disconnected newer IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip:              newerIP,
			expectedUpdated: true,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				tracker.mostRecentTrackedIPs[newerIP.NodeID] = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
				return tracker
			}(),
		},
		{
			name: "connected newer IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				return tracker
			}(),
			ip:              newerIP,
			expectedUpdated: true,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				tracker.mostRecentTrackedIPs[newerIP.NodeID] = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
				delete(tracker.gossipableIndices, newerIP.NodeID)
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
			requireMetricsConsistent(t, test.initialState)
		})
	}
}

func TestIPTracker_Connected(t *testing.T) {
	newerIP := newerTestIP(ip)
	tests := []struct {
		name          string
		initialState  *ipTracker
		ip            *ips.ClaimedIPPort
		expectedState *ipTracker
	}{
		{
			name:         "non-validator",
			initialState: newTestIPTracker(t),
			ip:           ip,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.connected[ip.NodeID] = ip
				return tracker
			}(),
		},
		{
			name: "first known IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				return tracker
			}(),
			ip: ip,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.mostRecentTrackedIPs[ip.NodeID] = ip
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.connected[ip.NodeID] = ip
				tracker.gossipableIndices[ip.NodeID] = 0
				tracker.gossipableIPs = []*ips.ClaimedIPPort{
					ip,
				}
				return tracker
			}(),
		},
		{
			name: "connected with older IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			}(),
			ip: ip,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(newerIP))
				tracker.connected[ip.NodeID] = ip
				return tracker
			}(),
		},
		{
			name: "connected with newer IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip: newerIP,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				tracker.mostRecentTrackedIPs[newerIP.NodeID] = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
				tracker.connected[newerIP.NodeID] = newerIP
				tracker.gossipableIndices[newerIP.NodeID] = 0
				tracker.gossipableIPs = []*ips.ClaimedIPPort{
					newerIP,
				}
				return tracker
			}(),
		},
		{
			name: "connected with same IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip: ip,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				tracker.connected[ip.NodeID] = ip
				tracker.gossipableIndices[ip.NodeID] = 0
				tracker.gossipableIPs = []*ips.ClaimedIPPort{
					ip,
				}
				return tracker
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.initialState.Connected(test.ip)
			requireEqual(t, test.expectedState, test.initialState)
			requireMetricsConsistent(t, test.initialState)
		})
	}
}

func TestIPTracker_Disconnected(t *testing.T) {
	tests := []struct {
		name          string
		initialState  *ipTracker
		nodeID        ids.NodeID
		expectedState *ipTracker
	}{
		{
			name: "not tracked",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID:        ip.NodeID,
			expectedState: newTestIPTracker(t),
		},
		{
			name: "not gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.ManuallyTrack(ip.NodeID)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.ManuallyTrack(ip.NodeID)
				delete(tracker.connected, ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				delete(tracker.connected, ip.NodeID)
				delete(tracker.gossipableIndices, ip.NodeID)
				tracker.gossipableIPs = tracker.gossipableIPs[:0]
				return tracker
			}(),
		},
		{
			name: "non-latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				tracker.OnValidatorAdded(otherIP.NodeID, nil, ids.Empty, 0)
				tracker.Connected(otherIP)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				tracker.OnValidatorAdded(otherIP.NodeID, nil, ids.Empty, 0)
				tracker.Connected(otherIP)
				delete(tracker.connected, ip.NodeID)
				tracker.gossipableIndices = map[ids.NodeID]int{
					otherIP.NodeID: 0,
				}
				tracker.gossipableIPs = []*ips.ClaimedIPPort{
					otherIP,
				}
				return tracker
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.initialState.Disconnected(test.nodeID)
			requireEqual(t, test.expectedState, test.initialState)
			requireMetricsConsistent(t, test.initialState)
		})
	}
}

func TestIPTracker_OnValidatorAdded(t *testing.T) {
	newerIP := newerTestIP(ip)

	tests := []struct {
		name          string
		initialState  *ipTracker
		nodeID        ids.NodeID
		expectedState *ipTracker
	}{
		{
			name: "manually tracked",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.gossipableIDs.Add(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "manually tracked and connected with older IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.Connected(ip)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.Connected(ip)
				require.True(t, tracker.AddIP(newerIP))
				tracker.gossipableIDs.Add(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "manually gossiped",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyGossip(ip.NodeID)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyGossip(ip.NodeID)
				return tracker
			}(),
		},
		{
			name:         "disconnected",
			initialState: newTestIPTracker(t),
			nodeID:       ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.trackedIDs.Add(ip.NodeID)
				tracker.gossipableIDs.Add(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "connected",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.mostRecentTrackedIPs[ip.NodeID] = ip
				tracker.trackedIDs.Add(ip.NodeID)
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.gossipableIndices[ip.NodeID] = 0
				tracker.gossipableIPs = []*ips.ClaimedIPPort{
					ip,
				}
				tracker.gossipableIDs.Add(ip.NodeID)
				return tracker
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.initialState.OnValidatorAdded(test.nodeID, nil, ids.Empty, 0)
			requireEqual(t, test.expectedState, test.initialState)
			requireMetricsConsistent(t, test.initialState)
		})
	}
}

func TestIPTracker_OnValidatorRemoved(t *testing.T) {
	tests := []struct {
		name          string
		initialState  *ipTracker
		nodeID        ids.NodeID
		expectedState *ipTracker
	}{
		{
			name: "manually tracked not gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				tracker.gossipableIDs.Remove(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "manually tracked latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				delete(tracker.gossipableIndices, ip.NodeID)
				tracker.gossipableIPs = tracker.gossipableIPs[:0]
				tracker.gossipableIDs.Remove(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "manually gossiped",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyGossip(ip.NodeID)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyGossip(ip.NodeID)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				return tracker
			}(),
		},
		{
			name: "not gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				delete(tracker.mostRecentTrackedIPs, ip.NodeID)
				tracker.trackedIDs.Remove(ip.NodeID)
				tracker.gossipableIDs.Remove(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				delete(tracker.mostRecentTrackedIPs, ip.NodeID)
				tracker.trackedIDs.Remove(ip.NodeID)
				delete(tracker.gossipableIndices, ip.NodeID)
				tracker.gossipableIPs = tracker.gossipableIPs[:0]
				tracker.gossipableIDs.Remove(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "non-latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				tracker.OnValidatorAdded(otherIP.NodeID, nil, ids.Empty, 0)
				tracker.Connected(otherIP)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip)
				tracker.OnValidatorAdded(otherIP.NodeID, nil, ids.Empty, 0)
				tracker.Connected(otherIP)
				delete(tracker.mostRecentTrackedIPs, ip.NodeID)
				tracker.trackedIDs.Remove(ip.NodeID)
				tracker.gossipableIndices = map[ids.NodeID]int{
					otherIP.NodeID: 0,
				}
				tracker.gossipableIPs = []*ips.ClaimedIPPort{
					otherIP,
				}
				tracker.gossipableIDs.Remove(ip.NodeID)
				return tracker
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.initialState.OnValidatorRemoved(test.nodeID, 0)
			requireEqual(t, test.expectedState, test.initialState)
			requireMetricsConsistent(t, test.initialState)
		})
	}
}

func TestIPTracker_GetGossipableIPs(t *testing.T) {
	require := require.New(t)

	tracker := newTestIPTracker(t)
	tracker.Connected(ip)
	tracker.Connected(otherIP)
	tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
	tracker.OnValidatorAdded(otherIP.NodeID, nil, ids.Empty, 0)

	gossipableIPs := tracker.GetGossipableIPs(ids.EmptyNodeID, bloom.EmptyFilter, nil, 2)
	require.ElementsMatch([]*ips.ClaimedIPPort{ip, otherIP}, gossipableIPs)

	gossipableIPs = tracker.GetGossipableIPs(ip.NodeID, bloom.EmptyFilter, nil, 2)
	require.Equal([]*ips.ClaimedIPPort{otherIP}, gossipableIPs)

	gossipableIPs = tracker.GetGossipableIPs(ids.EmptyNodeID, bloom.FullFilter, nil, 2)
	require.Empty(gossipableIPs)

	filter, err := bloom.New(8, 1024)
	require.NoError(err)
	bloom.Add(filter, ip.GossipID[:], nil)

	readFilter, err := bloom.Parse(filter.Marshal())
	require.NoError(err)

	gossipableIPs = tracker.GetGossipableIPs(ip.NodeID, readFilter, nil, 2)
	require.Equal([]*ips.ClaimedIPPort{otherIP}, gossipableIPs)
}

func TestIPTracker_BloomFiltersEverything(t *testing.T) {
	require := require.New(t)

	tracker := newTestIPTracker(t)
	tracker.Connected(ip)
	tracker.Connected(otherIP)
	tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
	tracker.OnValidatorAdded(otherIP.NodeID, nil, ids.Empty, 0)

	bloomBytes, salt := tracker.Bloom()
	readFilter, err := bloom.Parse(bloomBytes)
	require.NoError(err)

	gossipableIPs := tracker.GetGossipableIPs(ids.EmptyNodeID, readFilter, salt, 2)
	require.Empty(gossipableIPs)

	require.NoError(tracker.ResetBloom())
}

func TestIPTracker_BloomGrows(t *testing.T) {
	tests := []struct {
		name string
		add  func(tracker *ipTracker)
	}{
		{
			name: "Add Validator",
			add: func(tracker *ipTracker) {
				tracker.OnValidatorAdded(ids.GenerateTestNodeID(), nil, ids.Empty, 0)
			},
		},
		{
			name: "Manually Track",
			add: func(tracker *ipTracker) {
				tracker.ManuallyTrack(ids.GenerateTestNodeID())
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			tracker := newTestIPTracker(t)
			initialMaxBloomCount := tracker.maxBloomCount
			for i := 0; i < 2048; i++ {
				test.add(tracker)
			}
			requireMetricsConsistent(t, tracker)

			require.NoError(tracker.ResetBloom())
			require.Greater(tracker.maxBloomCount, initialMaxBloomCount)
			requireMetricsConsistent(t, tracker)
		})
	}
}

func TestIPTracker_BloomResetsDynamically(t *testing.T) {
	require := require.New(t)

	tracker := newTestIPTracker(t)
	tracker.Connected(ip)
	tracker.OnValidatorAdded(ip.NodeID, nil, ids.Empty, 0)
	tracker.OnValidatorRemoved(ip.NodeID, 0)
	tracker.maxBloomCount = 1
	tracker.Connected(otherIP)
	tracker.OnValidatorAdded(otherIP.NodeID, nil, ids.Empty, 0)
	requireMetricsConsistent(t, tracker)

	bloomBytes, salt := tracker.Bloom()
	readFilter, err := bloom.Parse(bloomBytes)
	require.NoError(err)

	require.False(bloom.Contains(readFilter, ip.GossipID[:], salt))
	require.True(bloom.Contains(readFilter, otherIP.GossipID[:], salt))
}

func TestIPTracker_PreventBloomFilterAddition(t *testing.T) {
	require := require.New(t)

	newerIP := newerTestIP(ip)
	newestIP := newerTestIP(newerIP)

	tracker := newTestIPTracker(t)
	tracker.ManuallyGossip(ip.NodeID)
	require.True(tracker.AddIP(ip))
	require.True(tracker.AddIP(newerIP))
	require.True(tracker.AddIP(newestIP))
	require.Equal(maxIPEntriesPerNode, tracker.bloomAdditions[ip.NodeID])
	requireMetricsConsistent(t, tracker)
}

func TestIPTracker_ShouldVerifyIP(t *testing.T) {
	require := require.New(t)

	newerIP := newerTestIP(ip)

	tracker := newTestIPTracker(t)
	require.False(tracker.ShouldVerifyIP(ip))
	tracker.ManuallyTrack(ip.NodeID)
	require.True(tracker.ShouldVerifyIP(ip))
	tracker.ManuallyGossip(ip.NodeID)
	require.True(tracker.ShouldVerifyIP(ip))
	require.True(tracker.AddIP(ip))
	require.False(tracker.ShouldVerifyIP(ip))
	require.True(tracker.ShouldVerifyIP(newerIP))
}

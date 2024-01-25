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
	require.Equal(expected.connected, actual.connected)
	require.Equal(expected.mostRecentValidatorIPs, actual.mostRecentValidatorIPs)
	require.Equal(expected.validators, actual.validators)
	require.Equal(expected.gossipableIndicies, actual.gossipableIndicies)
	require.Equal(expected.gossipableIPs, actual.gossipableIPs)
	require.Equal(expected.bloomAdditions, actual.bloomAdditions)
	require.Equal(expected.maxBloomCount, actual.maxBloomCount)
}

func requireMetricsConsistent(t *testing.T, tracker *ipTracker) {
	require := require.New(t)
	require.Equal(float64(len(tracker.mostRecentValidatorIPs)), testutil.ToFloat64(tracker.numValidatorIPs))
	require.Equal(float64(len(tracker.gossipableIPs)), testutil.ToFloat64(tracker.numGossipable))
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
				tracker.validators.Add(ip.NodeID)
				tracker.manuallyTracked.Add(ip.NodeID)
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
				tracker.mostRecentValidatorIPs[ip.NodeID] = ip
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.gossipableIndicies[ip.NodeID] = 0
				tracker.gossipableIPs = []*ips.ClaimedIPPort{
					ip,
				}
				tracker.validators.Add(ip.NodeID)
				tracker.manuallyTracked.Add(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "non-connected validator",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.manuallyTracked.Add(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "connected validator",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.onValidatorAdded(ip.NodeID)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.onValidatorAdded(ip.NodeID)
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
				tracker.onValidatorAdded(ip.NodeID)
				return tracker
			}(),
			ip:              ip,
			expectedUpdated: true,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.mostRecentValidatorIPs[ip.NodeID] = ip
				tracker.bloomAdditions[ip.NodeID] = 1
				return tracker
			}(),
		},
		{
			name: "older IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(newerIP.NodeID)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			}(),
			ip:              ip,
			expectedUpdated: false,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(newerIP.NodeID)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			}(),
		},
		{
			name: "same IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip:              ip,
			expectedUpdated: false,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
		},
		{
			name: "disconnected newer IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip:              newerIP,
			expectedUpdated: true,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				require.True(t, tracker.AddIP(ip))
				tracker.mostRecentValidatorIPs[newerIP.NodeID] = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
				return tracker
			}(),
		},
		{
			name: "connected newer IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				return tracker
			}(),
			ip:              newerIP,
			expectedUpdated: true,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				tracker.mostRecentValidatorIPs[newerIP.NodeID] = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
				delete(tracker.gossipableIndicies, newerIP.NodeID)
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
				tracker.onValidatorAdded(ip.NodeID)
				return tracker
			}(),
			ip: ip,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.connected[ip.NodeID] = ip
				tracker.mostRecentValidatorIPs[ip.NodeID] = ip
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.gossipableIndicies[ip.NodeID] = 0
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
				tracker.onValidatorAdded(newerIP.NodeID)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			}(),
			ip: ip,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(newerIP.NodeID)
				require.True(t, tracker.AddIP(newerIP))
				tracker.connected[ip.NodeID] = ip
				return tracker
			}(),
		},
		{
			name: "connected with newer IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip: newerIP,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				require.True(t, tracker.AddIP(ip))
				tracker.connected[newerIP.NodeID] = newerIP
				tracker.mostRecentValidatorIPs[newerIP.NodeID] = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
				tracker.gossipableIndicies[newerIP.NodeID] = 0
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
				tracker.onValidatorAdded(ip.NodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip: ip,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				require.True(t, tracker.AddIP(ip))
				tracker.connected[ip.NodeID] = ip
				tracker.gossipableIndicies[ip.NodeID] = 0
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
			name: "not gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID:        ip.NodeID,
			expectedState: newTestIPTracker(t),
		},
		{
			name: "latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				delete(tracker.connected, ip.NodeID)
				delete(tracker.gossipableIndicies, ip.NodeID)
				tracker.gossipableIPs = tracker.gossipableIPs[:0]
				return tracker
			}(),
		},
		{
			name: "non-latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				tracker.onValidatorAdded(otherIP.NodeID)
				tracker.Connected(otherIP)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				tracker.onValidatorAdded(otherIP.NodeID)
				tracker.Connected(otherIP)
				delete(tracker.connected, ip.NodeID)
				tracker.gossipableIndicies = map[ids.NodeID]int{
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
				return tracker
			}(),
		},
		{
			name:         "disconnected",
			initialState: newTestIPTracker(t),
			nodeID:       ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.validators.Add(ip.NodeID)
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
				tracker.validators.Add(ip.NodeID)
				tracker.mostRecentValidatorIPs[ip.NodeID] = ip
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.gossipableIndicies[ip.NodeID] = 0
				tracker.gossipableIPs = []*ips.ClaimedIPPort{
					ip,
				}
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
			name: "manually tracked",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				return tracker
			}(),
		},
		{
			name: "not gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				require.True(t, tracker.AddIP(ip))
				delete(tracker.mostRecentValidatorIPs, ip.NodeID)
				tracker.validators.Remove(ip.NodeID)
				return tracker
			}(),
		},
		{
			name: "latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				delete(tracker.mostRecentValidatorIPs, ip.NodeID)
				tracker.validators.Remove(ip.NodeID)
				delete(tracker.gossipableIndicies, ip.NodeID)
				tracker.gossipableIPs = tracker.gossipableIPs[:0]
				return tracker
			}(),
		},
		{
			name: "non-latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				tracker.onValidatorAdded(otherIP.NodeID)
				tracker.Connected(otherIP)
				return tracker
			}(),
			nodeID: ip.NodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(ip.NodeID)
				tracker.Connected(ip)
				tracker.onValidatorAdded(otherIP.NodeID)
				tracker.Connected(otherIP)
				delete(tracker.mostRecentValidatorIPs, ip.NodeID)
				tracker.validators.Remove(ip.NodeID)
				tracker.gossipableIndicies = map[ids.NodeID]int{
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
	tracker.onValidatorAdded(ip.NodeID)
	tracker.onValidatorAdded(otherIP.NodeID)

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
	tracker.onValidatorAdded(ip.NodeID)
	tracker.onValidatorAdded(otherIP.NodeID)

	bloomBytes, salt := tracker.Bloom()
	readFilter, err := bloom.Parse(bloomBytes)
	require.NoError(err)

	gossipableIPs := tracker.GetGossipableIPs(ids.EmptyNodeID, readFilter, salt, 2)
	require.Empty(gossipableIPs)

	require.NoError(tracker.ResetBloom())
}

func TestIPTracker_BloomGrowsWithValidatorSet(t *testing.T) {
	require := require.New(t)

	tracker := newTestIPTracker(t)
	initialMaxBloomCount := tracker.maxBloomCount
	for i := 0; i < 2048; i++ {
		tracker.onValidatorAdded(ids.GenerateTestNodeID())
	}
	requireMetricsConsistent(t, tracker)

	require.NoError(tracker.ResetBloom())
	require.Greater(tracker.maxBloomCount, initialMaxBloomCount)
	requireMetricsConsistent(t, tracker)
}

func TestIPTracker_BloomResetsDynamically(t *testing.T) {
	require := require.New(t)

	tracker := newTestIPTracker(t)
	tracker.Connected(ip)
	tracker.onValidatorAdded(ip.NodeID)
	tracker.OnValidatorRemoved(ip.NodeID, 0)
	tracker.maxBloomCount = 1
	tracker.Connected(otherIP)
	tracker.onValidatorAdded(otherIP.NodeID)
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
	tracker.onValidatorAdded(ip.NodeID)
	require.True(tracker.AddIP(ip))
	require.True(tracker.AddIP(newerIP))
	require.True(tracker.AddIP(newestIP))
	require.Equal(maxIPEntriesPerValidator, tracker.bloomAdditions[ip.NodeID])
	requireMetricsConsistent(t, tracker)
}

func TestIPTracker_ShouldVerifyIP(t *testing.T) {
	require := require.New(t)

	newerIP := newerTestIP(ip)

	tracker := newTestIPTracker(t)
	require.False(tracker.ShouldVerifyIP(ip))
	tracker.onValidatorAdded(ip.NodeID)
	require.True(tracker.ShouldVerifyIP(ip))
	require.True(tracker.AddIP(ip))
	require.False(tracker.ShouldVerifyIP(ip))
	require.True(tracker.ShouldVerifyIP(newerIP))
}

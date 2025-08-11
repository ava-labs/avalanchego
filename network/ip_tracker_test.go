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
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

func newTestIPTracker(t *testing.T) *ipTracker {
	tracker, err := newIPTracker(
		nil,
		logging.NoLog{},
		prometheus.NewRegistry(),
		false,
	)
	require.NoError(t, err)
	return tracker
}

func newerTestIP(ip *ips.ClaimedIPPort) *ips.ClaimedIPPort {
	return ips.NewClaimedIPPort(
		ip.Cert,
		ip.AddrPort,
		ip.Timestamp+1,
		ip.Signature,
	)
}

func requireEqual(t *testing.T, expected, actual *ipTracker) {
	require := require.New(t)
	require.Equal(expected.tracked, actual.tracked)
	require.Equal(expected.bloomAdditions, actual.bloomAdditions)
	require.Equal(expected.maxBloomCount, actual.maxBloomCount)
	require.Equal(expected.connected, actual.connected)
	require.Equal(expected.subnet, actual.subnet)
}

func requireMetricsConsistent(t *testing.T, tracker *ipTracker) {
	require := require.New(t)
	require.Equal(float64(len(tracker.tracked)), testutil.ToFloat64(tracker.numTrackedPeers))
	var numGossipableIPs int
	for _, subnet := range tracker.subnet {
		numGossipableIPs += len(subnet.gossipableIndices)
	}
	require.Equal(float64(numGossipableIPs), testutil.ToFloat64(tracker.numGossipableIPs))
	require.Equal(float64(len(tracker.subnet)), testutil.ToFloat64(tracker.numTrackedSubnets))
	require.Equal(float64(tracker.bloom.Count()), testutil.ToFloat64(tracker.bloomMetrics.Count))
	require.Equal(float64(tracker.maxBloomCount), testutil.ToFloat64(tracker.bloomMetrics.MaxCount))
}

func TestIPTracker_ManuallyTrack(t *testing.T) {
	subnetID := ids.GenerateTestID()
	tests := []struct {
		name           string
		initialState   func(t *testing.T) *ipTracker
		expectedChange func(*ipTracker)
	}{
		{
			name:         "non-connected non-validator",
			initialState: newTestIPTracker,
			expectedChange: func(tracker *ipTracker) {
				tracker.numTrackedPeers.Inc()
				tracker.tracked[ip.NodeID] = &trackedNode{
					manuallyTracked: true,
				}
			},
		},
		{
			name: "connected non-validator",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			expectedChange: func(tracker *ipTracker) {
				tracker.numTrackedPeers.Inc()
				tracker.tracked[ip.NodeID] = &trackedNode{
					manuallyTracked: true,
					ip:              ip,
				}
				tracker.bloomAdditions[ip.NodeID] = 1
			},
		},
		{
			name: "non-connected tracked validator",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[ip.NodeID].manuallyTracked = true
			},
		},
		{
			name: "non-connected untracked validator",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[ip.NodeID].manuallyTracked = true
			},
		},
		{
			name: "connected tracked validator",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[ip.NodeID].manuallyTracked = true
			},
		},
		{
			name: "connected untracked validator",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[ip.NodeID].manuallyTracked = true
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testState := test.initialState(t)
			expectedState := test.initialState(t)

			testState.ManuallyTrack(ip.NodeID)
			test.expectedChange(expectedState)

			requireEqual(t, expectedState, testState)
			requireMetricsConsistent(t, testState)
		})
	}
}

func TestIPTracker_ManuallyGossip(t *testing.T) {
	subnetID := ids.GenerateTestID()
	tests := []struct {
		name           string
		initialState   func(t *testing.T) *ipTracker
		subnetID       ids.ID
		expectedChange func(*ipTracker)
	}{
		{
			name:         "non-connected tracked non-validator",
			initialState: newTestIPTracker,
			subnetID:     constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numTrackedPeers.Inc()
				tracker.numTrackedSubnets.Inc()
				tracker.tracked[ip.NodeID] = &trackedNode{
					manuallyTracked:  true,
					validatedSubnets: set.Of(constants.PrimaryNetworkID),
					trackedSubnets:   set.Of(constants.PrimaryNetworkID),
				}
				tracker.subnet[constants.PrimaryNetworkID] = &gossipableSubnet{
					numGossipableIPs:   tracker.numGossipableIPs,
					manuallyGossipable: set.Of(ip.NodeID),
					gossipableIDs:      set.Of(ip.NodeID),
					gossipableIndices:  make(map[ids.NodeID]int),
				}
			},
		},
		{
			name:         "non-connected untracked non-validator",
			initialState: newTestIPTracker,
			subnetID:     subnetID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numTrackedPeers.Inc()
				tracker.numTrackedSubnets.Inc()
				tracker.tracked[ip.NodeID] = &trackedNode{
					validatedSubnets: set.Of(subnetID),
				}
				tracker.subnet[subnetID] = &gossipableSubnet{
					numGossipableIPs:   tracker.numGossipableIPs,
					manuallyGossipable: set.Of(ip.NodeID),
					gossipableIDs:      set.Of(ip.NodeID),
					gossipableIndices:  make(map[ids.NodeID]int),
				}
			},
		},
		{
			name: "connected tracked non-validator",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			subnetID: constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numTrackedPeers.Inc()
				tracker.numGossipableIPs.Inc()
				tracker.numTrackedSubnets.Inc()
				tracker.tracked[ip.NodeID] = &trackedNode{
					manuallyTracked:  true,
					validatedSubnets: set.Of(constants.PrimaryNetworkID),
					trackedSubnets:   set.Of(constants.PrimaryNetworkID),
					ip:               ip,
				}
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.subnet[constants.PrimaryNetworkID] = &gossipableSubnet{
					numGossipableIPs:   tracker.numGossipableIPs,
					manuallyGossipable: set.Of(ip.NodeID),
					gossipableIDs:      set.Of(ip.NodeID),
					gossipableIndices: map[ids.NodeID]int{
						ip.NodeID: 0,
					},
					gossipableIPs: []*ips.ClaimedIPPort{
						ip,
					},
				}
			},
		},
		{
			name: "connected untracked non-validator",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			subnetID: subnetID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numTrackedPeers.Inc()
				tracker.numTrackedSubnets.Inc()
				tracker.tracked[ip.NodeID] = &trackedNode{
					validatedSubnets: set.Of(subnetID),
					ip:               ip,
				}
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.subnet[subnetID] = &gossipableSubnet{
					numGossipableIPs:   tracker.numGossipableIPs,
					manuallyGossipable: set.Of(ip.NodeID),
					gossipableIDs:      set.Of(ip.NodeID),
					gossipableIndices:  make(map[ids.NodeID]int),
				}
			},
		},
		{
			name: "non-connected tracked validator",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			subnetID: constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[ip.NodeID].manuallyTracked = true
				tracker.subnet[constants.PrimaryNetworkID].manuallyGossipable = set.Of(ip.NodeID)
			},
		},
		{
			name: "non-connected untracked validator",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			subnetID: subnetID,
			expectedChange: func(tracker *ipTracker) {
				tracker.subnet[subnetID].manuallyGossipable = set.Of(ip.NodeID)
			},
		},
		{
			name: "connected tracked validator",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			subnetID: constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[ip.NodeID].manuallyTracked = true
				tracker.subnet[constants.PrimaryNetworkID].manuallyGossipable = set.Of(ip.NodeID)
			},
		},
		{
			name: "connected untracked validator",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			subnetID: subnetID,
			expectedChange: func(tracker *ipTracker) {
				tracker.subnet[subnetID].manuallyGossipable = set.Of(ip.NodeID)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testState := test.initialState(t)
			expectedState := test.initialState(t)

			testState.ManuallyGossip(test.subnetID, ip.NodeID)
			test.expectedChange(expectedState)

			requireEqual(t, expectedState, testState)
			requireMetricsConsistent(t, testState)
		})
	}
}

func TestIPTracker_ShouldVerifyIP(t *testing.T) {
	newerIP := newerTestIP(ip)
	tests := []struct {
		name                          string
		tracker                       func(t *testing.T) *ipTracker
		ip                            *ips.ClaimedIPPort
		expectedTrackAllSubnets       bool
		expectedTrackRequestedSubnets bool
	}{
		{
			name:                          "node not tracked",
			tracker:                       newTestIPTracker,
			ip:                            ip,
			expectedTrackAllSubnets:       false,
			expectedTrackRequestedSubnets: false,
		},
		{
			name: "undesired connection",
			tracker: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(ids.GenerateTestID(), ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			ip:                            ip,
			expectedTrackAllSubnets:       true,
			expectedTrackRequestedSubnets: false,
		},
		{
			name: "desired connection first IP",
			tracker: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			ip:                            ip,
			expectedTrackAllSubnets:       true,
			expectedTrackRequestedSubnets: true,
		},
		{
			name: "desired connection older IP",
			tracker: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			},
			ip:                            ip,
			expectedTrackAllSubnets:       false,
			expectedTrackRequestedSubnets: false,
		},
		{
			name: "desired connection same IP",
			tracker: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			},
			ip:                            ip,
			expectedTrackAllSubnets:       false,
			expectedTrackRequestedSubnets: false,
		},
		{
			name: "desired connection newer IP",
			tracker: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			},
			ip:                            newerIP,
			expectedTrackAllSubnets:       true,
			expectedTrackRequestedSubnets: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			tracker := test.tracker(t)
			require.Equal(test.expectedTrackAllSubnets, tracker.ShouldVerifyIP(test.ip, true))
			require.Equal(test.expectedTrackRequestedSubnets, tracker.ShouldVerifyIP(test.ip, false))
		})
	}
}

func TestIPTracker_AddIP(t *testing.T) {
	subnetID := ids.GenerateTestID()
	newerIP := newerTestIP(ip)
	tests := []struct {
		name                      string
		initialState              func(t *testing.T) *ipTracker
		ip                        *ips.ClaimedIPPort
		expectedChange            func(*ipTracker)
		expectedUpdatedAndDesired bool
	}{
		{
			name:                      "non-validator",
			initialState:              newTestIPTracker,
			ip:                        ip,
			expectedChange:            func(*ipTracker) {},
			expectedUpdatedAndDesired: false,
		},
		{
			name: "first known IP of tracked node",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			ip: ip,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[ip.NodeID].ip = ip
				tracker.bloomAdditions[ip.NodeID] = 1
			},
			expectedUpdatedAndDesired: true,
		},
		{
			name: "first known IP of untracked node",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			ip:                        ip,
			expectedUpdatedAndDesired: false,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[ip.NodeID].ip = ip
				tracker.bloomAdditions[ip.NodeID] = 1
			},
		},
		{
			name: "older IP of tracked node",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			},
			ip:                        ip,
			expectedUpdatedAndDesired: false,
			expectedChange:            func(*ipTracker) {},
		},
		{
			name: "older IP of untracked node",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				require.False(t, tracker.AddIP(newerIP))
				return tracker
			},
			ip:                        ip,
			expectedUpdatedAndDesired: false,
			expectedChange:            func(*ipTracker) {},
		},
		{
			name: "same IP of tracked node",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			},
			ip:                        ip,
			expectedUpdatedAndDesired: false,
			expectedChange:            func(*ipTracker) {},
		},
		{
			name: "same IP of untracked node",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				require.False(t, tracker.AddIP(ip))
				return tracker
			},
			ip:                        ip,
			expectedUpdatedAndDesired: false,
			expectedChange:            func(*ipTracker) {},
		},
		{
			name: "disconnected newer IP of tracked node",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			},
			ip:                        newerIP,
			expectedUpdatedAndDesired: true,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[newerIP.NodeID].ip = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
			},
		},
		{
			name: "disconnected newer IP of untracked node",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				require.False(t, tracker.AddIP(ip))
				return tracker
			},
			ip:                        newerIP,
			expectedUpdatedAndDesired: false,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[newerIP.NodeID].ip = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
			},
		},
		{
			name: "connected newer IP of tracked node",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			ip:                        newerIP,
			expectedUpdatedAndDesired: true,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[newerIP.NodeID].ip = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
				tracker.subnet[constants.PrimaryNetworkID].gossipableIPs[0] = newerIP
			},
		},
		{
			name: "connected newer IP of untracked node",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			ip:                        newerIP,
			expectedUpdatedAndDesired: false,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[newerIP.NodeID].ip = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testState := test.initialState(t)
			expectedState := test.initialState(t)

			updated := testState.AddIP(test.ip)
			test.expectedChange(expectedState)

			require.Equal(t, test.expectedUpdatedAndDesired, updated)
			requireEqual(t, expectedState, testState)
			requireMetricsConsistent(t, testState)
		})
	}
}

func TestIPTracker_Connected(t *testing.T) {
	subnetID := ids.GenerateTestID()
	newerIP := newerTestIP(ip)
	tests := []struct {
		name           string
		initialState   func(t *testing.T) *ipTracker
		ip             *ips.ClaimedIPPort
		expectedChange func(*ipTracker)
	}{
		{
			name:         "non-validator",
			initialState: newTestIPTracker,
			ip:           ip,
			expectedChange: func(tracker *ipTracker) {
				tracker.connected[ip.NodeID] = &connectedNode{
					trackedSubnets: set.Of(constants.PrimaryNetworkID),
					ip:             ip,
				}
			},
		},
		{
			name: "first known IP of node tracking subnet",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			ip: ip,
			expectedChange: func(tracker *ipTracker) {
				tracker.numGossipableIPs.Inc()
				tracker.tracked[ip.NodeID].ip = ip
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.connected[ip.NodeID] = &connectedNode{
					trackedSubnets: set.Of(constants.PrimaryNetworkID),
					ip:             ip,
				}

				subnet := tracker.subnet[constants.PrimaryNetworkID]
				subnet.gossipableIndices[ip.NodeID] = 0
				subnet.gossipableIPs = []*ips.ClaimedIPPort{
					ip,
				}
			},
		},
		{
			name: "first known IP of node not tracking subnet",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			ip: ip,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[ip.NodeID].ip = ip
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.connected[ip.NodeID] = &connectedNode{
					trackedSubnets: set.Of(constants.PrimaryNetworkID),
					ip:             ip,
				}
			},
		},
		{
			name: "connected with older IP of node tracking subnet",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			},
			ip: ip,
			expectedChange: func(tracker *ipTracker) {
				tracker.numGossipableIPs.Inc()
				tracker.connected[ip.NodeID] = &connectedNode{
					trackedSubnets: set.Of(constants.PrimaryNetworkID),
					ip:             ip,
				}

				subnet := tracker.subnet[constants.PrimaryNetworkID]
				subnet.gossipableIndices[newerIP.NodeID] = 0
				subnet.gossipableIPs = []*ips.ClaimedIPPort{
					newerIP,
				}
			},
		},
		{
			name: "connected with older IP of node not tracking subnet",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				require.False(t, tracker.AddIP(newerIP))
				return tracker
			},
			ip: ip,
			expectedChange: func(tracker *ipTracker) {
				tracker.connected[ip.NodeID] = &connectedNode{
					trackedSubnets: set.Of(constants.PrimaryNetworkID),
					ip:             ip,
				}
			},
		},
		{
			name: "connected with newer IP of node tracking subnet",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			},
			ip: newerIP,
			expectedChange: func(tracker *ipTracker) {
				tracker.numGossipableIPs.Inc()
				tracker.tracked[newerIP.NodeID].ip = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
				tracker.connected[newerIP.NodeID] = &connectedNode{
					trackedSubnets: set.Of(constants.PrimaryNetworkID),
					ip:             newerIP,
				}

				subnet := tracker.subnet[constants.PrimaryNetworkID]
				subnet.gossipableIndices[newerIP.NodeID] = 0
				subnet.gossipableIPs = []*ips.ClaimedIPPort{
					newerIP,
				}
			},
		},
		{
			name: "connected with newer IP of node not tracking subnet",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				require.False(t, tracker.AddIP(ip))
				return tracker
			},
			ip: newerIP,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[newerIP.NodeID].ip = newerIP
				tracker.bloomAdditions[newerIP.NodeID] = 2
				tracker.connected[newerIP.NodeID] = &connectedNode{
					trackedSubnets: set.Of(constants.PrimaryNetworkID),
					ip:             newerIP,
				}
			},
		},
		{
			name: "connected with same IP of node tracking subnet",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			},
			ip: ip,
			expectedChange: func(tracker *ipTracker) {
				tracker.numGossipableIPs.Inc()
				tracker.connected[ip.NodeID] = &connectedNode{
					trackedSubnets: set.Of(constants.PrimaryNetworkID),
					ip:             ip,
				}

				subnet := tracker.subnet[constants.PrimaryNetworkID]
				subnet.gossipableIndices[ip.NodeID] = 0
				subnet.gossipableIPs = []*ips.ClaimedIPPort{
					ip,
				}
			},
		},
		{
			name: "connected with same IP of node not tracking subnet",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				require.False(t, tracker.AddIP(ip))
				return tracker
			},
			ip: ip,
			expectedChange: func(tracker *ipTracker) {
				tracker.connected[ip.NodeID] = &connectedNode{
					trackedSubnets: set.Of(constants.PrimaryNetworkID),
					ip:             ip,
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testState := test.initialState(t)
			expectedState := test.initialState(t)

			testState.Connected(test.ip, set.Of(constants.PrimaryNetworkID))
			test.expectedChange(expectedState)

			requireEqual(t, expectedState, testState)
			requireMetricsConsistent(t, testState)
		})
	}
}

func TestIPTracker_Disconnected(t *testing.T) {
	subnetID := ids.GenerateTestID()
	tests := []struct {
		name           string
		initialState   func(t *testing.T) *ipTracker
		expectedChange func(*ipTracker)
	}{
		{
			name: "not gossipable",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			expectedChange: func(*ipTracker) {},
		},
		{
			name: "latest gossipable",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			expectedChange: func(tracker *ipTracker) {
				tracker.numGossipableIPs.Dec()
				delete(tracker.connected, ip.NodeID)

				subnet := tracker.subnet[constants.PrimaryNetworkID]
				delete(subnet.gossipableIndices, ip.NodeID)
				subnet.gossipableIPs = subnet.gossipableIPs[:0]
			},
		},
		{
			name: "non-latest gossipable",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, otherIP.NodeID, nil, ids.Empty, 0)
				tracker.Connected(otherIP, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			expectedChange: func(tracker *ipTracker) {
				tracker.numGossipableIPs.Dec()
				delete(tracker.connected, ip.NodeID)

				subnet := tracker.subnet[constants.PrimaryNetworkID]
				subnet.gossipableIndices = map[ids.NodeID]int{
					otherIP.NodeID: 0,
				}
				subnet.gossipableIPs = []*ips.ClaimedIPPort{
					otherIP,
				}
			},
		},
		{
			name: "remove multiple gossipable IPs",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID, subnetID))
				return tracker
			},
			expectedChange: func(tracker *ipTracker) {
				tracker.numGossipableIPs.Add(-2)
				delete(tracker.connected, ip.NodeID)

				primarySubnet := tracker.subnet[constants.PrimaryNetworkID]
				delete(primarySubnet.gossipableIndices, ip.NodeID)
				primarySubnet.gossipableIPs = primarySubnet.gossipableIPs[:0]

				subnet := tracker.subnet[subnetID]
				delete(subnet.gossipableIndices, ip.NodeID)
				subnet.gossipableIPs = subnet.gossipableIPs[:0]
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testState := test.initialState(t)
			expectedState := test.initialState(t)

			testState.Disconnected(ip.NodeID)
			expectedState.Disconnected(ip.NodeID)

			requireEqual(t, expectedState, testState)
			requireMetricsConsistent(t, testState)
		})
	}
}

func TestIPTracker_OnValidatorAdded(t *testing.T) {
	newerIP := newerTestIP(ip)
	subnetID := ids.GenerateTestID()
	tests := []struct {
		name           string
		initialState   func(t *testing.T) *ipTracker
		subnetID       ids.ID
		expectedChange func(*ipTracker)
	}{
		{
			name: "manually tracked",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				return tracker
			},
			subnetID: constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[ip.NodeID].validatedSubnets.Add(constants.PrimaryNetworkID)
				tracker.tracked[ip.NodeID].trackedSubnets.Add(constants.PrimaryNetworkID)
				tracker.subnet[constants.PrimaryNetworkID] = &gossipableSubnet{
					numGossipableIPs:  tracker.numGossipableIPs,
					gossipableIDs:     set.Of(ip.NodeID),
					gossipableIndices: make(map[ids.NodeID]int),
				}
			},
		},
		{
			name: "manually tracked and connected",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			subnetID: constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numGossipableIPs.Inc()
				tracker.tracked[ip.NodeID].validatedSubnets.Add(constants.PrimaryNetworkID)
				tracker.tracked[ip.NodeID].trackedSubnets.Add(constants.PrimaryNetworkID)
				tracker.subnet[constants.PrimaryNetworkID] = &gossipableSubnet{
					numGossipableIPs: tracker.numGossipableIPs,
					gossipableIDs:    set.Of(ip.NodeID),
					gossipableIndices: map[ids.NodeID]int{
						ip.NodeID: 0,
					},
					gossipableIPs: []*ips.ClaimedIPPort{
						ip,
					},
				}
			},
		},
		{
			name: "manually tracked and connected with older IP",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			},
			subnetID: constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numGossipableIPs.Inc()
				tracker.tracked[ip.NodeID].validatedSubnets.Add(constants.PrimaryNetworkID)
				tracker.tracked[ip.NodeID].trackedSubnets.Add(constants.PrimaryNetworkID)
				tracker.subnet[constants.PrimaryNetworkID] = &gossipableSubnet{
					numGossipableIPs: tracker.numGossipableIPs,
					gossipableIDs:    set.Of(ip.NodeID),
					gossipableIndices: map[ids.NodeID]int{
						ip.NodeID: 0,
					},
					gossipableIPs: []*ips.ClaimedIPPort{
						newerIP,
					},
				}
			},
		},
		{
			name: "manually gossiped",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyGossip(constants.PrimaryNetworkID, ip.NodeID)
				return tracker
			},
			subnetID:       constants.PrimaryNetworkID,
			expectedChange: func(*ipTracker) {},
		},
		{
			name:         "disconnected",
			initialState: newTestIPTracker,
			subnetID:     constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.tracked[ip.NodeID] = &trackedNode{
					validatedSubnets: set.Of(constants.PrimaryNetworkID),
					trackedSubnets:   set.Of(constants.PrimaryNetworkID),
				}
				tracker.subnet[constants.PrimaryNetworkID] = &gossipableSubnet{
					numGossipableIPs:  tracker.numGossipableIPs,
					gossipableIDs:     set.Of(ip.NodeID),
					gossipableIndices: make(map[ids.NodeID]int),
				}
			},
		},
		{
			name: "connected",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			subnetID: constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numGossipableIPs.Inc()
				tracker.tracked[ip.NodeID] = &trackedNode{
					validatedSubnets: set.Of(constants.PrimaryNetworkID),
					trackedSubnets:   set.Of(constants.PrimaryNetworkID),
					ip:               ip,
				}
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.subnet[constants.PrimaryNetworkID] = &gossipableSubnet{
					numGossipableIPs: tracker.numGossipableIPs,
					gossipableIDs:    set.Of(ip.NodeID),
					gossipableIndices: map[ids.NodeID]int{
						ip.NodeID: 0,
					},
					gossipableIPs: []*ips.ClaimedIPPort{
						ip,
					},
				}
			},
		},
		{
			name: "connected to other subnet",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			subnetID: subnetID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numTrackedSubnets.Inc()
				tracker.tracked[ip.NodeID] = &trackedNode{
					validatedSubnets: set.Of(subnetID),
					ip:               ip,
				}
				tracker.bloomAdditions[ip.NodeID] = 1
				tracker.subnet[subnetID] = &gossipableSubnet{
					numGossipableIPs:  tracker.numGossipableIPs,
					gossipableIDs:     set.Of(ip.NodeID),
					gossipableIndices: make(map[ids.NodeID]int),
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testState := test.initialState(t)
			expectedState := test.initialState(t)

			testState.OnValidatorAdded(test.subnetID, ip.NodeID, nil, ids.Empty, 0)
			test.expectedChange(expectedState)

			requireEqual(t, expectedState, testState)
			requireMetricsConsistent(t, testState)
		})
	}
}

func TestIPTracker_OnValidatorRemoved(t *testing.T) {
	subnetID := ids.GenerateTestID()
	tests := []struct {
		name           string
		initialState   func(t *testing.T) *ipTracker
		subnetID       ids.ID
		expectedChange func(*ipTracker)
	}{
		{
			name: "remove last validator of subnet",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				return tracker
			},
			subnetID: constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numTrackedPeers.Dec()
				tracker.numTrackedSubnets.Dec()
				delete(tracker.tracked, ip.NodeID)
				delete(tracker.subnet, constants.PrimaryNetworkID)
			},
		},
		{
			name: "manually tracked not gossipable",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				require.True(t, tracker.AddIP(ip))
				return tracker
			},
			subnetID: constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numTrackedSubnets.Dec()

				node := tracker.tracked[ip.NodeID]
				node.validatedSubnets.Remove(constants.PrimaryNetworkID)
				node.trackedSubnets.Remove(constants.PrimaryNetworkID)

				delete(tracker.subnet, constants.PrimaryNetworkID)
			},
		},
		{
			name: "manually tracked latest gossipable",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(ip.NodeID)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			subnetID: constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numGossipableIPs.Dec()
				tracker.numTrackedSubnets.Dec()

				node := tracker.tracked[ip.NodeID]
				node.validatedSubnets.Remove(constants.PrimaryNetworkID)
				node.trackedSubnets.Remove(constants.PrimaryNetworkID)

				delete(tracker.subnet, constants.PrimaryNetworkID)
			},
		},
		{
			name: "manually gossiped",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyGossip(constants.PrimaryNetworkID, ip.NodeID)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			subnetID:       constants.PrimaryNetworkID,
			expectedChange: func(*ipTracker) {},
		},
		{
			name: "manually gossiped on other subnet",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyGossip(constants.PrimaryNetworkID, ip.NodeID)
				tracker.OnValidatorAdded(subnetID, ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			subnetID: subnetID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numTrackedSubnets.Dec()
				tracker.tracked[ip.NodeID].validatedSubnets.Remove(subnetID)
				delete(tracker.subnet, subnetID)
			},
		},
		{
			name: "non-latest gossipable",
			initialState: func(t *testing.T) *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
				tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, otherIP.NodeID, nil, ids.Empty, 0)
				tracker.Connected(otherIP, set.Of(constants.PrimaryNetworkID))
				return tracker
			},
			subnetID: constants.PrimaryNetworkID,
			expectedChange: func(tracker *ipTracker) {
				tracker.numTrackedPeers.Dec()
				tracker.numGossipableIPs.Dec()
				delete(tracker.tracked, ip.NodeID)

				subnet := tracker.subnet[constants.PrimaryNetworkID]
				subnet.gossipableIDs.Remove(ip.NodeID)
				subnet.gossipableIndices = map[ids.NodeID]int{
					otherIP.NodeID: 0,
				}
				subnet.gossipableIPs = []*ips.ClaimedIPPort{
					otherIP,
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testState := test.initialState(t)
			expectedState := test.initialState(t)

			testState.OnValidatorRemoved(test.subnetID, ip.NodeID, 0)
			test.expectedChange(expectedState)

			requireEqual(t, expectedState, testState)
			requireMetricsConsistent(t, testState)
		})
	}
}

func TestIPTracker_BloomGrows(t *testing.T) {
	tests := []struct {
		name string
		add  func(tracker *ipTracker)
	}{
		{
			name: "Add Validator",
			add: func(tracker *ipTracker) {
				tracker.OnValidatorAdded(constants.PrimaryNetworkID, ids.GenerateTestNodeID(), nil, ids.Empty, 0)
			},
		},
		{
			name: "Manually Track",
			add: func(tracker *ipTracker) {
				tracker.ManuallyTrack(ids.GenerateTestNodeID())
			},
		},
		{
			name: "Manually Gossip",
			add: func(tracker *ipTracker) {
				tracker.ManuallyGossip(ids.GenerateTestID(), ids.GenerateTestNodeID())
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
	tracker.Connected(ip, set.Of(constants.PrimaryNetworkID))
	tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
	tracker.OnValidatorRemoved(constants.PrimaryNetworkID, ip.NodeID, 0)

	tracker.maxBloomCount = 1
	tracker.Connected(otherIP, set.Of(constants.PrimaryNetworkID))
	tracker.OnValidatorAdded(constants.PrimaryNetworkID, otherIP.NodeID, nil, ids.Empty, 0)
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
	tracker.ManuallyGossip(constants.PrimaryNetworkID, ip.NodeID)
	require.True(tracker.AddIP(ip))
	require.True(tracker.AddIP(newerIP))
	require.True(tracker.AddIP(newestIP))
	require.Equal(maxIPEntriesPerNode, tracker.bloomAdditions[ip.NodeID])
	requireMetricsConsistent(t, tracker)
}

func TestIPTracker_GetGossipableIPs(t *testing.T) {
	subnetIDA := ids.GenerateTestID()
	subnetIDB := ids.GenerateTestID()
	unknownSubnetID := ids.GenerateTestID()

	tracker := newTestIPTracker(t)
	tracker.Connected(ip, set.Of(constants.PrimaryNetworkID, subnetIDA))
	tracker.Connected(otherIP, set.Of(constants.PrimaryNetworkID, subnetIDA, subnetIDB))
	tracker.OnValidatorAdded(constants.PrimaryNetworkID, ip.NodeID, nil, ids.Empty, 0)
	tracker.OnValidatorAdded(subnetIDA, otherIP.NodeID, nil, ids.Empty, 0)
	tracker.OnValidatorAdded(subnetIDB, otherIP.NodeID, nil, ids.Empty, 0)

	myFilterBytes, mySalt := tracker.Bloom()
	myFilter, err := bloom.Parse(myFilterBytes)
	require.NoError(t, err)

	tests := []struct {
		name      string
		toIterate set.Set[ids.ID]
		allowed   set.Set[ids.ID]
		nodeID    ids.NodeID
		filter    *bloom.ReadFilter
		salt      []byte
		expected  []*ips.ClaimedIPPort
	}{
		{
			name:      "fetch both subnets IPs",
			toIterate: set.Of(constants.PrimaryNetworkID, subnetIDA),
			allowed:   set.Of(constants.PrimaryNetworkID, subnetIDA),
			nodeID:    ids.EmptyNodeID,
			filter:    bloom.EmptyFilter,
			salt:      nil,
			expected:  []*ips.ClaimedIPPort{ip, otherIP},
		},
		{
			name:      "filter nodeID",
			toIterate: set.Of(constants.PrimaryNetworkID, subnetIDA),
			allowed:   set.Of(constants.PrimaryNetworkID, subnetIDA),
			nodeID:    ip.NodeID,
			filter:    bloom.EmptyFilter,
			salt:      nil,
			expected:  []*ips.ClaimedIPPort{otherIP},
		},
		{
			name:      "filter duplicate nodeIDs",
			toIterate: set.Of(subnetIDA, subnetIDB),
			allowed:   set.Of(subnetIDA, subnetIDB),
			nodeID:    ids.EmptyNodeID,
			filter:    bloom.EmptyFilter,
			salt:      nil,
			expected:  []*ips.ClaimedIPPort{otherIP},
		},
		{
			name:      "filter known IPs",
			toIterate: set.Of(constants.PrimaryNetworkID, subnetIDA),
			allowed:   set.Of(constants.PrimaryNetworkID, subnetIDA),
			nodeID:    ids.EmptyNodeID,
			filter: func() *bloom.ReadFilter {
				filter, err := bloom.New(8, 1024)
				require.NoError(t, err)
				bloom.Add(filter, ip.GossipID[:], nil)

				readFilter, err := bloom.Parse(filter.Marshal())
				require.NoError(t, err)
				return readFilter
			}(),
			salt:     nil,
			expected: []*ips.ClaimedIPPort{otherIP},
		},
		{
			name:      "filter everything",
			toIterate: set.Of(constants.PrimaryNetworkID, subnetIDA, subnetIDB),
			allowed:   set.Of(constants.PrimaryNetworkID, subnetIDA, subnetIDB),
			nodeID:    ids.EmptyNodeID,
			filter:    myFilter,
			salt:      mySalt,
			expected:  nil,
		},
		{
			name:      "only fetch primary network IPs",
			toIterate: set.Of(constants.PrimaryNetworkID),
			allowed:   set.Of(constants.PrimaryNetworkID),
			nodeID:    ids.EmptyNodeID,
			filter:    bloom.EmptyFilter,
			salt:      nil,
			expected:  []*ips.ClaimedIPPort{ip},
		},
		{
			name:      "only fetch subnet IPs",
			toIterate: set.Of(subnetIDA),
			allowed:   set.Of(subnetIDA),
			nodeID:    ids.EmptyNodeID,
			filter:    bloom.EmptyFilter,
			salt:      nil,
			expected:  []*ips.ClaimedIPPort{otherIP},
		},
		{
			name:      "filter subnet",
			toIterate: set.Of(constants.PrimaryNetworkID, subnetIDA),
			allowed:   set.Of(constants.PrimaryNetworkID),
			nodeID:    ids.EmptyNodeID,
			filter:    bloom.EmptyFilter,
			salt:      nil,
			expected:  []*ips.ClaimedIPPort{ip},
		},
		{
			name:      "skip unknown subnet",
			toIterate: set.Of(unknownSubnetID),
			allowed:   set.Of(unknownSubnetID),
			nodeID:    ids.EmptyNodeID,
			filter:    bloom.EmptyFilter,
			salt:      nil,
			expected:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gossipableIPs := getGossipableIPs(
				tracker,
				test.toIterate,
				test.allowed.Contains,
				test.nodeID,
				test.filter,
				test.salt,
				2,
			)
			require.ElementsMatch(t, test.expected, gossipableIPs)
		})
	}
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	ip      *ips.ClaimedIPPort
	otherIP *ips.ClaimedIPPort
)

func init() {
	{
		cert, err := staking.NewTLSCert()
		if err != nil {
			panic(err)
		}
		ip = &ips.ClaimedIPPort{
			Cert: staking.CertificateFromX509(cert.Leaf),
			IPPort: ips.IPPort{
				IP:   net.IPv4(127, 0, 0, 1),
				Port: 9651,
			},
			Timestamp: 1,
		}
	}

	{
		cert, err := staking.NewTLSCert()
		if err != nil {
			panic(err)
		}
		otherIP = &ips.ClaimedIPPort{
			Cert: staking.CertificateFromX509(cert.Leaf),
			IPPort: ips.IPPort{
				IP:   net.IPv4(127, 0, 0, 1),
				Port: 9651,
			},
			Timestamp: 1,
		}
	}
}

func newTestIPTracker(t *testing.T) *ipTracker {
	tracker, err := newIPTracker(logging.NoLog{})
	require.NoError(t, err)
	return tracker
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
	nodeID := ip.NodeID()
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

func TestIPTracker_Connected(t *testing.T) {
	nodeID := ip.NodeID()
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
				tracker.connected[nodeID] = ip
				return tracker
			}(),
		},
		{
			name: "first known IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				return tracker
			}(),
			ip: ip,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.connected[nodeID] = ip
				tracker.mostRecentValidatorIPs[nodeID] = ip
				tracker.bloomAdditions[nodeID] = 1
				tracker.gossipableIndicies[nodeID] = 0
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
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(newerIP))
				return tracker
			}(),
			ip: ip,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(newerIP))
				tracker.connected[nodeID] = ip
				return tracker
			}(),
		},
		{
			name: "connected with newer IP",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip: newerIP,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(ip))
				tracker.connected[nodeID] = newerIP
				tracker.mostRecentValidatorIPs[nodeID] = newerIP
				tracker.bloomAdditions[nodeID] = 2
				tracker.gossipableIndicies[nodeID] = 0
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
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			ip: ip,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(ip))
				tracker.connected[nodeID] = ip
				tracker.gossipableIndicies[nodeID] = 0
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
		})
	}
}

func TestIPTracker_Disconnected(t *testing.T) {
	nodeID := ip.NodeID()
	otherNodeID := otherIP.NodeID()
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
			nodeID:        nodeID,
			expectedState: newTestIPTracker(t),
		},
		{
			name: "latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				delete(tracker.connected, nodeID)
				delete(tracker.gossipableIndicies, nodeID)
				tracker.gossipableIPs = tracker.gossipableIPs[:0]
				return tracker
			}(),
		},
		{
			name: "non-latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				tracker.onValidatorAdded(otherNodeID)
				tracker.Connected(otherIP)
				return tracker
			}(),
			nodeID: nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				tracker.onValidatorAdded(otherNodeID)
				tracker.Connected(otherIP)
				delete(tracker.connected, nodeID)
				tracker.gossipableIndicies = map[ids.NodeID]int{
					otherNodeID: 0,
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
		})
	}
}

func TestIPTracker_OnValidatorAdded(t *testing.T) {
	nodeID := ip.NodeID()
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
				tracker.ManuallyTrack(nodeID)
				return tracker
			}(),
			nodeID: nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(nodeID)
				return tracker
			}(),
		},
		{
			name:         "disconnected",
			initialState: newTestIPTracker(t),
			nodeID:       nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.validators.Add(nodeID)
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
			nodeID: nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.Connected(ip)
				tracker.validators.Add(nodeID)
				tracker.mostRecentValidatorIPs[nodeID] = ip
				tracker.bloomAdditions[nodeID] = 1
				tracker.gossipableIndicies[nodeID] = 0
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
		})
	}
}

func TestIPTracker_OnValidatorRemoved(t *testing.T) {
	nodeID := ip.NodeID()
	otherNodeID := otherIP.NodeID()
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
				tracker.ManuallyTrack(nodeID)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.ManuallyTrack(nodeID)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				return tracker
			}(),
		},
		{
			name: "not gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(ip))
				return tracker
			}(),
			nodeID: nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				require.True(t, tracker.AddIP(ip))
				delete(tracker.mostRecentValidatorIPs, nodeID)
				tracker.validators.Remove(nodeID)
				return tracker
			}(),
		},
		{
			name: "latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				return tracker
			}(),
			nodeID: nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				delete(tracker.mostRecentValidatorIPs, nodeID)
				tracker.validators.Remove(nodeID)
				delete(tracker.gossipableIndicies, nodeID)
				tracker.gossipableIPs = tracker.gossipableIPs[:0]
				return tracker
			}(),
		},
		{
			name: "non-latest gossipable",
			initialState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				tracker.onValidatorAdded(otherNodeID)
				tracker.Connected(otherIP)
				return tracker
			}(),
			nodeID: nodeID,
			expectedState: func() *ipTracker {
				tracker := newTestIPTracker(t)
				tracker.onValidatorAdded(nodeID)
				tracker.Connected(ip)
				tracker.onValidatorAdded(otherNodeID)
				tracker.Connected(otherIP)
				delete(tracker.mostRecentValidatorIPs, nodeID)
				tracker.validators.Remove(nodeID)
				tracker.gossipableIndicies = map[ids.NodeID]int{
					otherNodeID: 0,
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
		})
	}
}

func TestIPTracker_GetGossipableIPs(t *testing.T) {
	require := require.New(t)

	nodeID := ip.NodeID()
	gossipID := ip.GossipID()
	otherNodeID := otherIP.NodeID()

	tracker := newTestIPTracker(t)
	tracker.Connected(ip)
	tracker.Connected(otherIP)
	tracker.onValidatorAdded(nodeID)
	tracker.onValidatorAdded(otherNodeID)

	gossipableIPs := tracker.GetGossipableIPs(ids.EmptyNodeID, bloom.EmptyFilter, nil, 2)
	require.ElementsMatch([]*ips.ClaimedIPPort{ip, otherIP}, gossipableIPs)

	gossipableIPs = tracker.GetGossipableIPs(nodeID, bloom.EmptyFilter, nil, 2)
	require.Equal([]*ips.ClaimedIPPort{otherIP}, gossipableIPs)

	gossipableIPs = tracker.GetGossipableIPs(ids.EmptyNodeID, bloom.FullFilter, nil, 2)
	require.Empty(gossipableIPs)

	filter, err := bloom.New(8, 1024)
	require.NoError(err)
	bloom.Add(filter, gossipID[:], nil)

	readFilter, err := bloom.Parse(filter.Marshal())
	require.NoError(err)

	gossipableIPs = tracker.GetGossipableIPs(nodeID, readFilter, nil, 2)
	require.Equal([]*ips.ClaimedIPPort{otherIP}, gossipableIPs)
}

func TestIPTracker_BloomFiltersEverything(t *testing.T) {
	require := require.New(t)

	nodeID := ip.NodeID()
	otherNodeID := otherIP.NodeID()

	tracker := newTestIPTracker(t)
	tracker.Connected(ip)
	tracker.Connected(otherIP)
	tracker.onValidatorAdded(nodeID)
	tracker.onValidatorAdded(otherNodeID)

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

	require.NoError(tracker.ResetBloom())
	require.Greater(tracker.maxBloomCount, initialMaxBloomCount)
}

func TestIPTracker_BloomResetsDynamically(t *testing.T) {
	require := require.New(t)

	nodeID := ip.NodeID()
	gossipID := ip.GossipID()
	otherNodeID := otherIP.NodeID()
	otherGossipID := otherIP.GossipID()

	tracker := newTestIPTracker(t)
	tracker.Connected(ip)
	tracker.onValidatorAdded(nodeID)
	tracker.OnValidatorRemoved(nodeID, 0)
	tracker.maxBloomCount = 1
	tracker.Connected(otherIP)
	tracker.onValidatorAdded(otherNodeID)

	bloomBytes, salt := tracker.Bloom()
	readFilter, err := bloom.Parse(bloomBytes)
	require.NoError(err)

	require.True(bloom.Contains(readFilter, otherGossipID[:], salt))
	require.False(bloom.Contains(readFilter, gossipID[:], salt))
}

func TestIPTracker_PreventBloomFilterAddition(t *testing.T) {
	require := require.New(t)

	nodeID := ip.NodeID()
	newerIP := newerTestIP(ip)
	newestIP := newerTestIP(newerIP)

	tracker := newTestIPTracker(t)
	tracker.onValidatorAdded(nodeID)
	require.True(tracker.AddIP(ip))
	require.True(tracker.AddIP(newerIP))
	require.True(tracker.AddIP(newestIP))
	require.Equal(maxIPEntriesPerValidator, tracker.bloomAdditions[nodeID])
}

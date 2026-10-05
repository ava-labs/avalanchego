// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestAllowedPeers(t *testing.T) {
	var (
		active  atomic.Bool
		allowed atomic.Pointer[set.Set[ids.NodeID]]
	)
	config := defaultConfig
	config.AllowedPeers = func() (set.Set[ids.NodeID], bool) {
		if !active.Load() {
			return nil, false
		}
		return *allowed.Load(), true
	}

	nodeIDs, networks, eg := newFullyConnectedTestNetworkWithConfig(
		t,
		[]router.InboundHandler{nil, nil, nil},
		config,
	)
	allowedSet := set.Of(nodeIDs[0], nodeIDs[1])
	allowed.Store(&allowedSet)

	require.True(t, networks[0].AllowConnection(nodeIDs[2]), "AllowConnection() before activation")

	active.Store(true)
	require.False(t, networks[0].AllowConnection(nodeIDs[2]), "AllowConnection(disallowed)")
	require.True(t, networks[0].AllowConnection(nodeIDs[1]), "AllowConnection(allowed)")

	networks[0].DisconnectDisallowed()
	require.Eventually(t, func() bool {
		return len(networks[0].PeerInfo([]ids.NodeID{nodeIDs[2]})) == 0
	}, 10*time.Second, 50*time.Millisecond, "disallowed peer still connected")
	require.Len(t, networks[0].PeerInfo([]ids.NodeID{nodeIDs[1]}), 1, "allowed peer disconnected")

	for _, net := range networks {
		net.StartClose()
	}
	require.NoError(t, eg.Wait(), "eg.Wait()")
}

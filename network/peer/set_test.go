// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestSet(t *testing.T) {
	require := require.New(t)

	set := NewSet()

	peer1 := &peer{
		id:              ids.NodeID{0x01},
		observedUptimes: map[ids.ID]uint32{constants.PrimaryNetworkID: 0},
	}
	updatedPeer1 := &peer{
		id:              ids.NodeID{0x01},
		observedUptimes: map[ids.ID]uint32{constants.PrimaryNetworkID: 1},
	}
	peer2 := &peer{
		id: ids.NodeID{0x02},
	}
	unknownPeer := &peer{
		id: ids.NodeID{0xff},
	}
	peer3 := &peer{
		id: ids.NodeID{0x03},
	}
	peer4 := &peer{
		id: ids.NodeID{0x04},
	}

	// add of first peer is handled
	set.Add(peer1)
	retrievedPeer1, peer1Found := set.GetByID(peer1.id)
	require.True(peer1Found)
	observed1, _ := peer1.ObservedUptime(constants.PrimaryNetworkID)
	observed2, _ := retrievedPeer1.ObservedUptime(constants.PrimaryNetworkID)
	require.Equal(observed1, observed2)
	require.Equal(1, set.Len())

	// re-addition of peer works as update
	set.Add(updatedPeer1)
	retrievedPeer1, peer1Found = set.GetByID(peer1.id)
	require.True(peer1Found)
	observed1, _ = updatedPeer1.ObservedUptime(constants.PrimaryNetworkID)
	observed2, _ = retrievedPeer1.ObservedUptime(constants.PrimaryNetworkID)
	require.Equal(observed1, observed2)
	require.Equal(1, set.Len())

	// add of another peer is handled
	set.Add(peer2)
	retrievedPeer2, peer2Found := set.GetByID(peer2.id)
	require.True(peer2Found)
	observed1, _ = peer2.ObservedUptime(constants.PrimaryNetworkID)
	observed2, _ = retrievedPeer2.ObservedUptime(constants.PrimaryNetworkID)
	require.Equal(observed1, observed2)
	require.Equal(2, set.Len())

	// removal of added peer is handled
	set.Remove(peer1.id)
	_, peer1Found = set.GetByID(peer1.id)
	require.False(peer1Found)
	retrievedPeer2, peer2Found = set.GetByID(peer2.id)
	require.True(peer2Found)
	require.Equal(peer2.id, retrievedPeer2.ID())
	require.Equal(1, set.Len())

	// query for unknown peer is handled
	_, unknownPeerfound := set.GetByID(unknownPeer.id)
	require.False(unknownPeerfound)

	// removal of unknown peer is handled
	set.Remove(unknownPeer.id)
	retrievedPeer2, peer2Found = set.GetByID(peer2.id)
	require.True(peer2Found)
	require.Equal(peer2.id, retrievedPeer2.ID())
	require.Equal(1, set.Len())

	// retrival by inbound index is handled
	set.Add(peer3)
	set.Add(peer4)
	require.Equal(3, set.Len())

	thirdPeer, ok := set.GetByIndex(1)
	require.True(ok)
	require.Equal(peer3.id, thirdPeer.ID())

	// retrival by out-of-bounds index is handled
	_, ok = set.GetByIndex(3)
	require.False(ok)
}

func TestSetSample(t *testing.T) {
	require := require.New(t)

	set := NewSet()

	peer1 := &peer{
		id: ids.NodeID{0x01},
	}
	peer2 := &peer{
		id: ids.NodeID{0x02},
	}

	// Case: Empty
	peers := set.Sample(0, NoPrecondition)
	require.Empty(peers)

	peers = set.Sample(-1, NoPrecondition)
	require.Empty(peers)

	peers = set.Sample(1, NoPrecondition)
	require.Empty(peers)

	// Case: 1 peer
	set.Add(peer1)

	peers = set.Sample(0, NoPrecondition)
	require.Empty(peers)

	peers = set.Sample(1, NoPrecondition)
	require.Equal(peers, []Peer{peer1})

	peers = set.Sample(2, NoPrecondition)
	require.Equal(peers, []Peer{peer1})

	// Case: 2 peers
	set.Add(peer2)

	peers = set.Sample(1, NoPrecondition)
	require.Len(peers, 1)
}

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/stretchr/testify/assert"
)

func TestPeersData(t *testing.T) {
	data := peersData{}
	data.initialize()

	peer1 := peer{
		nodeID: ids.ShortID{0x01},
	}

	// add of first peer is handled
	data.add(&peer1)
	retrievedPeer1, peer1Found := data.getByID(peer1.nodeID)
	assert.True(t, peer1Found)
	assert.True(t, &peer1 == retrievedPeer1)
	assert.True(t, data.size() == 1)

	// re-addition of peer works as update
	updatedPeer1 := peer{
		nodeID: ids.ShortID{0x01},
	}
	data.add(&updatedPeer1)
	retrievedPeer1, peer1Found = data.getByID(peer1.nodeID)
	assert.True(t, peer1Found)
	assert.True(t, &updatedPeer1 == retrievedPeer1)
	assert.True(t, data.size() == 1)

	peer2 := peer{
		nodeID: ids.ShortID{0x02},
	}

	// add of another peer is handled
	data.add(&peer2)
	retrievedPeer2, peer2Found := data.getByID(peer2.nodeID)
	assert.True(t, peer2Found)
	assert.True(t, &peer2 == retrievedPeer2)
	assert.True(t, data.size() == 2)

	// removal of added peer is handled
	data.remove(&peer1)
	retrievedPeer1, peer1Found = data.getByID(peer1.nodeID)
	assert.False(t, peer1Found)
	assert.True(t, retrievedPeer1 == nil)
	retrievedPeer2, peer2Found = data.getByID(peer2.nodeID)
	assert.True(t, peer2Found)
	assert.True(t, &peer2 == retrievedPeer2)
	assert.True(t, data.size() == 1)

	unknownPeer := peer{
		nodeID: ids.ShortID{0xff},
	}

	// query for unknown peer is handled
	retrievedUnknownPeer, unknownPeerfound := data.getByID(unknownPeer.nodeID)
	assert.False(t, unknownPeerfound)
	assert.True(t, retrievedUnknownPeer == nil)

	// removal of unknown peer is handled
	data.remove(&unknownPeer)
	retrievedPeer2, peer2Found = data.getByID(peer2.nodeID)
	assert.True(t, peer2Found)
	assert.True(t, &peer2 == retrievedPeer2)
	assert.True(t, data.size() == 1)

	// retrival by inbound index is handled
	peer3 := peer{
		nodeID: ids.ShortID{0x03},
	}
	peer4 := peer{
		nodeID: ids.ShortID{0x04},
	}
	data.add(&peer3)
	data.add(&peer4)
	assert.True(t, data.size() == 3)

	thirdPeer, ok := data.getByIdx(1)
	assert.True(t, ok)
	assert.True(t, &peer3 == thirdPeer)

	// retrival by outbound index is handled
	outOfIndexPeer, ok := data.getByIdx(data.size())
	assert.False(t, ok)
	assert.True(t, outOfIndexPeer == nil)

	// reset is idempotent
	data.reset()
	assert.True(t, data.size() == 0)

	data.reset()
	assert.True(t, data.size() == 0)
}

func TestPeersDataSample(t *testing.T) {
	data := peersData{}
	data.initialize()
	trackedSubnetIDs := ids.Set{}
	trackedSubnetIDs.Add(constants.PrimaryNetworkID)
	networkWithValidators := &network{config: &Config{Validators: validators.NewManager()}}
	// Case: Empty
	peers, err := data.sample(constants.PrimaryNetworkID, false, 0)
	assert.NoError(t, err)
	assert.Len(t, peers, 0)

	peers, err = data.sample(constants.PrimaryNetworkID, false, 1)
	assert.NoError(t, err)
	assert.Len(t, peers, 0)

	// Case: 1 peer who hasn't finished handshake
	peer1 := peer{
		nodeID:         ids.ShortID{0x01},
		trackedSubnets: trackedSubnetIDs,
		net:            networkWithValidators,
	}
	data.add(&peer1)
	peers, err = data.sample(constants.PrimaryNetworkID, false, 0)
	assert.NoError(t, err)
	assert.Len(t, peers, 0)

	peers, err = data.sample(constants.PrimaryNetworkID, false, 1)
	assert.NoError(t, err)
	assert.Len(t, peers, 0)

	// Case-1: peer who hasn't finished handshake, 1 who has
	peer2 := peer{
		nodeID:         ids.ShortID{0x02},
		trackedSubnets: trackedSubnetIDs,
	}
	peer2.finishedHandshake.SetValue(true)
	data.add(&peer2)

	peers, err = data.sample(constants.PrimaryNetworkID, false, 0)
	assert.NoError(t, err)
	assert.Len(t, peers, 0)

	peers, err = data.sample(constants.PrimaryNetworkID, false, 1)
	assert.NoError(t, err)
	assert.Len(t, peers, 1)
	assert.EqualValues(t, peers[0].nodeID, peer2.nodeID)

	peers, err = data.sample(constants.PrimaryNetworkID, false, 2)
	assert.NoError(t, err)
	assert.Len(t, peers, 1)
	assert.EqualValues(t, peers[0].nodeID, peer2.nodeID)

	// Case-2: peers who have finished handshake
	peer1.finishedHandshake.SetValue(true)
	peers, err = data.sample(constants.PrimaryNetworkID, false, 0)
	assert.NoError(t, err)
	assert.Len(t, peers, 0)

	peers, err = data.sample(constants.PrimaryNetworkID, false, 1)
	assert.NoError(t, err)
	assert.Len(t, peers, 1)

	peers, err = data.sample(constants.PrimaryNetworkID, false, 2)
	assert.NoError(t, err)
	assert.Len(t, peers, 2)
	// Ensure both peers are sampled once
	assert.True(t,
		(peers[0].nodeID == peer1.nodeID && peers[1].nodeID == peer2.nodeID) ||
			(peers[0].nodeID == peer2.nodeID && peers[1].nodeID == peer1.nodeID),
	)

	peers, err = data.sample(constants.PrimaryNetworkID, false, 3)
	assert.NoError(t, err)
	assert.Len(t, peers, 2)
	// Ensure both peers are sampled once
	assert.True(t,
		(peers[0].nodeID == peer1.nodeID && peers[1].nodeID == peer2.nodeID) ||
			(peers[0].nodeID == peer2.nodeID && peers[1].nodeID == peer1.nodeID),
	)

	// Case-3: peers who track testSubnet
	testSubnetID := ids.GenerateTestID()

	// no peers has this subnet
	peers, err = data.sample(testSubnetID, false, 3)
	assert.NoError(t, err)
	assert.Len(t, peers, 0)

	// peer with additional subnet sampled
	newSubnetSet := ids.Set{}
	newSubnetSet.Add(constants.PrimaryNetworkID, testSubnetID)

	peer3 := peer{
		nodeID:         ids.ShortID{0x03},
		trackedSubnets: newSubnetSet,
	}
	peer3.finishedHandshake.SetValue(true)
	data.add(&peer3)

	peers, err = data.sample(testSubnetID, false, 3)
	assert.NoError(t, err)
	assert.Len(t, peers, 1)

	// Ensure peer is sampled
	assert.Equal(t, peer3.nodeID, peers[0].nodeID)

	peers, err = data.sample(constants.PrimaryNetworkID, false, 3)
	assert.NoError(t, err)
	assert.Len(t, peers, 3)
}

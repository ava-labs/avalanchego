// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/ids"
)

func TestSet(t *testing.T) {
	assert := assert.New(t)

	set := NewSet()

	peer1 := &peer{
		id:             ids.ShortID{0x01},
		observedUptime: 0,
	}
	updatedPeer1 := &peer{
		id:             ids.ShortID{0x01},
		observedUptime: 1,
	}
	peer2 := &peer{
		id: ids.ShortID{0x02},
	}
	unknownPeer := &peer{
		id: ids.ShortID{0xff},
	}
	peer3 := &peer{
		id: ids.ShortID{0x03},
	}
	peer4 := &peer{
		id: ids.ShortID{0x04},
	}

	// add of first peer is handled
	set.Add(peer1)
	retrievedPeer1, peer1Found := set.GetByID(peer1.id)
	assert.True(peer1Found)
	assert.Equal(peer1.ObservedUptime(), retrievedPeer1.ObservedUptime())
	assert.Equal(1, set.Len())

	// re-addition of peer works as update
	set.Add(updatedPeer1)
	retrievedPeer1, peer1Found = set.GetByID(peer1.id)
	assert.True(peer1Found)
	assert.Equal(updatedPeer1.ObservedUptime(), retrievedPeer1.ObservedUptime())
	assert.Equal(1, set.Len())

	// add of another peer is handled
	set.Add(peer2)
	retrievedPeer2, peer2Found := set.GetByID(peer2.id)
	assert.True(peer2Found)
	assert.Equal(peer2.ObservedUptime(), retrievedPeer2.ObservedUptime())
	assert.Equal(2, set.Len())

	// removal of added peer is handled
	set.Remove(peer1.id)
	_, peer1Found = set.GetByID(peer1.id)
	assert.False(peer1Found)
	retrievedPeer2, peer2Found = set.GetByID(peer2.id)
	assert.True(peer2Found)
	assert.Equal(peer2.id, retrievedPeer2.ID())
	assert.Equal(1, set.Len())

	// query for unknown peer is handled
	_, unknownPeerfound := set.GetByID(unknownPeer.id)
	assert.False(unknownPeerfound)

	// removal of unknown peer is handled
	set.Remove(unknownPeer.id)
	retrievedPeer2, peer2Found = set.GetByID(peer2.id)
	assert.True(peer2Found)
	assert.Equal(peer2.id, retrievedPeer2.ID())
	assert.Equal(1, set.Len())

	// retrival by inbound index is handled
	set.Add(peer3)
	set.Add(peer4)
	assert.Equal(3, set.Len())

	thirdPeer, ok := set.GetByIndex(1)
	assert.True(ok)
	assert.Equal(peer3.id, thirdPeer.ID())

	// retrival by out-of-bounds index is handled
	_, ok = set.GetByIndex(3)
	assert.False(ok)
}

func TestSetSample(t *testing.T) {
	assert := assert.New(t)

	set := NewSet()

	peer1 := &peer{
		id: ids.ShortID{0x01},
	}
	peer2 := &peer{
		id: ids.ShortID{0x02},
	}

	// Case: Empty
	peers := set.Sample(0, NoPrecondition)
	assert.Empty(peers)

	peers = set.Sample(-1, NoPrecondition)
	assert.Empty(peers)

	peers = set.Sample(1, NoPrecondition)
	assert.Empty(peers)

	// Case: 1 peer
	set.Add(peer1)

	peers = set.Sample(0, NoPrecondition)
	assert.Empty(peers)

	peers = set.Sample(1, NoPrecondition)
	assert.Equal(peers, []Peer{peer1})

	peers = set.Sample(2, NoPrecondition)
	assert.Equal(peers, []Peer{peer1})

	// Case: 2 peers
	set.Add(peer2)

	peers = set.Sample(1, NoPrecondition)
	assert.Len(peers, 1)
}

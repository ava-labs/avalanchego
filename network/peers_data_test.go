package network

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

func TestPeersData(t *testing.T) {
	data := peersData{}
	data.initialize()

	peer1 := peer{
		id: ids.ShortID{0x01},
	}

	// add of first peer is handled
	data.add(&peer1)
	retrievedPeer1, peer1Found := data.getByID(peer1.id)
	assert.True(t, peer1Found)
	assert.True(t, &peer1 == retrievedPeer1)
	assert.True(t, data.size() == 1)

	// re-addition of peer works as update
	updatedPeer1 := peer{
		id: ids.ShortID{0x01},
	}
	data.add(&updatedPeer1)
	retrievedPeer1, peer1Found = data.getByID(peer1.id)
	assert.True(t, peer1Found)
	assert.True(t, &updatedPeer1 == retrievedPeer1)
	assert.True(t, data.size() == 1)

	peer2 := peer{
		id: ids.ShortID{0x02},
	}

	// add of another peer is handled
	data.add(&peer2)
	retrievedPeer2, peer2Found := data.getByID(peer2.id)
	assert.True(t, peer2Found)
	assert.True(t, &peer2 == retrievedPeer2)
	assert.True(t, data.size() == 2)

	// removal of added peer is handled
	data.remove(&peer1)
	retrievedPeer1, peer1Found = data.getByID(peer1.id)
	assert.False(t, peer1Found)
	assert.True(t, retrievedPeer1 == nil)
	retrievedPeer2, peer2Found = data.getByID(peer2.id)
	assert.True(t, peer2Found)
	assert.True(t, &peer2 == retrievedPeer2)
	assert.True(t, data.size() == 1)

	unknownPeer := peer{
		id: ids.ShortID{0xff},
	}

	// query for unknown peer is handled
	retrievedUnknownPeer, unknownPeerfound := data.getByID(unknownPeer.id)
	assert.False(t, unknownPeerfound)
	assert.True(t, retrievedUnknownPeer == nil)

	// removal of unknown peer is handled
	data.remove(&unknownPeer)
	retrievedPeer2, peer2Found = data.getByID(peer2.id)
	assert.True(t, peer2Found)
	assert.True(t, &peer2 == retrievedPeer2)
	assert.True(t, data.size() == 1)

	// retrival by inbound index is handled
	peer3 := peer{
		id: ids.ShortID{0x03},
	}
	peer4 := peer{
		id: ids.ShortID{0x04},
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

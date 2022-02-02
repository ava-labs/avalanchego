// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consistent

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/hashing"

	"github.com/stretchr/testify/assert"

	"github.com/golang/mock/gomock"
)

var (
	_ Hashable = &testKey{}

	// nodes
	node1 = testKey{key: "node-1", hash: 1}
	node2 = testKey{key: "node-2", hash: 2}
	node3 = testKey{key: "node-3", hash: 3}
)

// testKey is a simple wrapper around a key and its mocked hash for testing.
type testKey struct {
	// key
	key string
	// mocked hash value of the key
	hash uint64
}

// ConsistentHashKey implements Hashable
func (t testKey) ConsistentHashKey() []byte {
	return []byte(t.key)
}

// Tests that a key routes to its closest clockwise node.
// Test cases are described in greater detail below; see diagrams for Ring.
func TestGetMapsToClockwiseNode(t *testing.T) {
	tests := []struct {
		// name of the test
		name string
		// nodes that exist in the ring
		ringNodes []testKey
		// key to try to route
		key testKey
		// expected key to be routed to
		expectedNode testKey
	}{
		{
			// If we're left of a node in the ring, we should route to it.
			//
			// Ring:
			// ... -> foo -> node-1 -> ...
			name: "key with right node",
			ringNodes: []testKey{
				node1,
			},
			key: testKey{
				key:  "foo",
				hash: 0,
			},
			expectedNode: node1,
		},
		{
			// If we occupy the same hash as the only ring node, we should route to it.
			//
			// Ring:
			// ... -> foo, node-1 -> ...
			name: "key with equal node",
			ringNodes: []testKey{
				node1,
			},
			key: testKey{
				key:  "foo",
				hash: 1,
			},
			expectedNode: node1,
		},
		{
			// If we're clockwise of the only node, we should wrap around and route to that node.
			//
			// Ring:
			// ... -> node-1 -> foo -> ...
			name: "key wraps around to left-most node",
			ringNodes: []testKey{
				node1,
			},
			key: testKey{
				key:  "foo",
				hash: 2,
			},
			expectedNode: node1,
		},

		{
			// If we're left of multiple nodes in the ring, we should route to the first clockwise node.
			//
			// Ring:
			// ... -> foo -> node-1 -> node-2 -> ...
			name: "key with two right nodes",
			ringNodes: []testKey{
				node1,
				node2,
			},
			key: testKey{
				key:  "foo",
				hash: 0,
			},
			expectedNode: node1,
		},
		{
			// If we occupy the same hash as a node, we should route to the node clockwise of it.
			//
			// Ring:
			// ... -> foo, node-1 -> node-2 -> ...
			name: "key with one equal node and one right node",
			ringNodes: []testKey{
				node2,
				node1,
			},
			key: testKey{
				key:  "foo",
				hash: 1,
			},
			expectedNode: node2,
		},
		{
			// If we're in between two nodes, we should route to the clockwise node.
			//
			// Ring:
			// ... -> node-1 -> foo -> node-3 -> ...
			name: "key between two nodes",
			ringNodes: []testKey{
				node3,
				node1,
			},
			key: testKey{
				key:  "foo",
				hash: 2,
			},
			expectedNode: node3,
		},
		{
			// If we're clockwise of all ring keys, we should wrap around and route to the left-most node.
			//
			// Ring:
			// ... -> node-1 -> node-2 -> foo -> ...
			name: "key with two left nodes and no right neighbors",
			ringNodes: []testKey{
				node2,
				node1,
			},
			key: testKey{
				key:  "foo",
				hash: 3,
			},
			expectedNode: node1,
		},
		{
			// If we occupy the same hash as a node, we should wrap around to the clockwise node.
			//
			// Ring:
			// ... -> node-1 -> node-2, foo -> ...
			name: "key with equal neighbor and no right node wraps around to left-most node",
			ringNodes: []testKey{
				node2,
				node1,
			},
			key: testKey{
				key:  "foo",
				hash: 2,
			},
			expectedNode: node1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ring, hasher, ctrl := setupTest(t, 1)
			defer ctrl.Finish()

			// setup expected calls
			calls := make([]*gomock.Call, len(test.ringNodes)+1)

			for i, key := range test.ringNodes {
				calls[i] = hasher.EXPECT().Hash(getHashKey(key.ConsistentHashKey(), 0)).Return(key.hash).Times(1)
			}

			calls[len(test.ringNodes)] = hasher.EXPECT().Hash(test.key.ConsistentHashKey()).Return(test.key.hash).Times(1)
			gomock.InOrder(calls...)

			// execute test
			for _, key := range test.ringNodes {
				ring.Add(key)
			}

			node, err := ring.Get(test.key)
			assert.Equal(t, test.expectedNode, node)
			assert.Nil(t, err)
		})
	}
}

// Tests that if we have an empty ring, trying to call Get results in an error, as there is no node to route to.
func TestGetOnEmptyRingReturnsError(t *testing.T) {
	ring, _, ctrl := setupTest(t, 1)
	defer ctrl.Finish()

	foo := testKey{
		key:  "foo",
		hash: 0,
	}

	node, err := ring.Get(foo)

	assert.Equal(t, nil, node)
	assert.Equal(t, errEmptyRing, err)
}

// Tests that trying to call Remove on a node that doesn't exist should return false.
func TestRemoveNonExistentKeyReturnsFalse(t *testing.T) {
	ring, hasher, ctrl := setupTest(t, 1)
	defer ctrl.Finish()

	gomock.InOrder(
		hasher.EXPECT().Hash(getHashKey(node1.ConsistentHashKey(), 0)).Return(uint64(1)).Times(1),
	)

	// try to remove something from an empty ring.
	assert.False(t, ring.Remove(node1))
}

// Tests that trying to call Remove on a node that doesn't exist should return true.
func TestRemoveExistingKeyReturnsTrue(t *testing.T) {
	ring, hasher, ctrl := setupTest(t, 1)
	defer ctrl.Finish()

	gomock.InOrder(
		hasher.EXPECT().Hash(getHashKey(node1.ConsistentHashKey(), 0)).Return(uint64(1)).Times(1),
	)

	// Add a node into the ring.
	//
	// Ring:
	// ... -> node-1 -> ...
	ring.Add(node1)

	gomock.InOrder(
		hasher.EXPECT().Hash(getHashKey(node1.ConsistentHashKey(), 0)).Return(uint64(1)).Times(1),
	)

	// Try to remove it.
	//
	// Ring:
	// ... -> (empty) -> ...
	assert.True(t, ring.Remove(node1))
}

// Tests that if we have a collision, the node is replaced.
func TestAddCollisionReplacement(t *testing.T) {
	ring, hasher, ctrl := setupTest(t, 1)
	defer ctrl.Finish()

	foo := testKey{
		key:  "foo",
		hash: 2,
	}

	gomock.InOrder(
		// node-1 and node-2 occupy the same hash
		hasher.EXPECT().Hash(getHashKey(node1.ConsistentHashKey(), 0)).Return(uint64(1)).Times(1),
		hasher.EXPECT().Hash(getHashKey(node2.ConsistentHashKey(), 0)).Return(uint64(1)).Times(1),
		hasher.EXPECT().Hash(foo.ConsistentHashKey()).Return(uint64(1)).Times(1),
	)

	// Ring:
	// ... -> node-1 -> ...
	ring.Add(node1)

	// Ring:
	// ... -> node-2 -> ...
	ring.Add(node2)

	ringMember, err := ring.Get(foo)

	assert.Equal(t, node2, ringMember)
	assert.Nil(t, err)
}

// Tests that virtual nodes are replicated on Add.
func TestAddVirtualNodes(t *testing.T) {
	ring, hasher, ctrl := setupTest(t, 3)
	defer ctrl.Finish()

	gomock.InOrder(
		// we should see 3 virtual nodes created (0, 1, 2) when we insert a node into the ring.

		// insert node-1
		hasher.EXPECT().Hash(getHashKey(node1.ConsistentHashKey(), 0)).Return(uint64(0)).Times(1),
		hasher.EXPECT().Hash(getHashKey(node1.ConsistentHashKey(), 1)).Return(uint64(2)).Times(1),
		hasher.EXPECT().Hash(getHashKey(node1.ConsistentHashKey(), 2)).Return(uint64(4)).Times(1),

		// insert node-2
		hasher.EXPECT().Hash(getHashKey(node2.ConsistentHashKey(), 0)).Return(uint64(1)).Times(1),
		hasher.EXPECT().Hash(getHashKey(node2.ConsistentHashKey(), 1)).Return(uint64(3)).Times(1),
		hasher.EXPECT().Hash(getHashKey(node2.ConsistentHashKey(), 2)).Return(uint64(5)).Times(1),

		// gets that should route to node-1
		hasher.EXPECT().Hash([]byte("foo1")).Return(uint64(1)).Times(1),
		hasher.EXPECT().Hash([]byte("foo3")).Return(uint64(3)).Times(1),
		hasher.EXPECT().Hash([]byte("foo5")).Return(uint64(5)).Times(1),

		// gets that should route to node-2
		hasher.EXPECT().Hash([]byte("foo0")).Return(uint64(0)).Times(1),
		hasher.EXPECT().Hash([]byte("foo2")).Return(uint64(2)).Times(1),
		hasher.EXPECT().Hash([]byte("foo4")).Return(uint64(4)).Times(1),
	)

	// Add node 1.
	//
	// Ring:
	// ... -> node-1-v0 -> node-1-v1 -> node-1-v2 -> ...
	ring.Add(node1)

	// Add node 2.
	//
	// Ring:
	// ... -> node-1-v0 -> node-2-v0 -> node-1-v1 -> node-2-v1 -> node-1-v2 -> node-2-v2 -> ...
	ring.Add(node2)

	// Gets that should route to node-1
	node, err := ring.Get(testKey{key: "foo1"})
	assert.Equal(t, node1, node)
	assert.Nil(t, err)
	node, err = ring.Get(testKey{key: "foo3"})
	assert.Equal(t, node1, node)
	assert.Nil(t, err)
	node, err = ring.Get(testKey{key: "foo5"})
	assert.Equal(t, node1, node)
	assert.Nil(t, err)

	// Gets that should route to node-2
	node, err = ring.Get(testKey{key: "foo0"})
	assert.Equal(t, node2, node)
	assert.Nil(t, err)
	node, err = ring.Get(testKey{key: "foo2"})
	assert.Equal(t, node2, node)
	assert.Nil(t, err)
	node, err = ring.Get(testKey{key: "foo4"})
	assert.Equal(t, node2, node)
	assert.Nil(t, err)
}

// Tests that the node routed to changes if an Add results in a key shuffle.
func TestGetShuffleOnAdd(t *testing.T) {
	ring, hasher, ctrl := setupTest(t, 1)
	defer ctrl.Finish()

	foo := testKey{
		key:  "foo",
		hash: 1,
	}

	gomock.InOrder(
		hasher.EXPECT().Hash(getHashKey(node1.ConsistentHashKey(), 0)).Return(uint64(0)).Times(1),
		hasher.EXPECT().Hash(foo.ConsistentHashKey()).Return(foo.hash).Times(1),

		hasher.EXPECT().Hash(getHashKey(node2.ConsistentHashKey(), 0)).Return(uint64(2)).Times(1),
		hasher.EXPECT().Hash(foo.ConsistentHashKey()).Return(foo.hash).Times(1),
	)

	// Add node-1 into the ring
	//
	// Ring:
	// ... -> node-1 -> ...
	ring.Add(node1)

	// node-1 is the closest clockwise node (when we wrap around), so we route to it.
	//
	// Ring:
	// ... -> node-1 -> foo -> ...
	node, err := ring.Get(foo)

	assert.Equal(t, node1, node)
	assert.Nil(t, err)

	// Add node-2, which results in foo being shuffled from node-1 to node-2.
	//
	// Ring:
	// ... -> node-1 -> node-2 -> ...
	ring.Add(node2)

	// Now node-2 is our closest clockwise node, so we should route to it.
	//
	// Ring:
	// ... -> node-1 -> foo -> node-2 -> ...
	node, err = ring.Get(foo)

	assert.Equal(t, node2, node)
	assert.Nil(t, err)
}

// Tests that we can iterate around the ring.
func TestIteration(t *testing.T) {
	ring, hasher, ctrl := setupTest(t, 1)
	defer ctrl.Finish()

	foo := testKey{
		key:  "foo",
		hash: 0,
	}

	gomock.InOrder(
		hasher.EXPECT().Hash(getHashKey(node1.ConsistentHashKey(), 0)).Return(node1.hash).Times(1),
		hasher.EXPECT().Hash(getHashKey(node2.ConsistentHashKey(), 0)).Return(node2.hash).Times(1),

		hasher.EXPECT().Hash(foo.ConsistentHashKey()).Return(foo.hash).Times(1),
		hasher.EXPECT().Hash(node1.ConsistentHashKey()).Return(node1.hash).Times(1),
	)

	// add node-1 into the ring
	//
	// Ring:
	// ... -> node-1 -> ...
	ring.Add(node1)

	// add node-2 into the ring
	//
	// Ring:
	// ... -> node-1 -> node-2 -> ...
	ring.Add(node2)

	// Get the neighbor of foo
	//
	// Ring:
	// ... -> foo -> node-1 -> node-2 -> ...
	node, err := ring.Get(foo)
	assert.Equal(t, node1, node)
	assert.Nil(t, err)

	// iterate by re-using node-1 to get node-2
	node, err = ring.Get(node)
	assert.Equal(t, node2, node)
	assert.Nil(t, err)
}

func setupTest(t *testing.T, virtualNodes int) (Ring, *hashing.MockHasher, *gomock.Controller) {
	ctrl := gomock.NewController(t)
	hasher := hashing.NewMockHasher(ctrl)

	return NewHashRing(RingConfig{
		VirtualNodes: virtualNodes,
		Hasher:       hasher,
		Degree:       2,
	}), hasher, ctrl
}

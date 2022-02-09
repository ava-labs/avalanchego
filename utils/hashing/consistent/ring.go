// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consistent

import (
	"errors"
	"sync"

	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/google/btree"
)

var (
	_ Ring       = &hashRing{}
	_ btree.Item = &ringItem{}

	errEmptyRing = errors.New("ring doesn't have any members")
)

// Ring is an interface for a consistent hashing ring
// (ref: https://en.wikipedia.org/wiki/Consistent_hashing).
//
// Consistent hashing is a method of distributing keys across an arbitrary set
// of destinations.
//
// Consider a naive approach, which uses modulo to map a key to a node to serve
// cached requests. Let N be the number of keys and M is the amount of possible
// hashing destinations. Here, a key would be routed to a node based on
// h(key) % M = node assigned.
//
// With this approach, we can route a key to a node in O(1) time, but this
// results in cache misses as nodes are introduced and removed, which results in
// the keys being reshuffled across nodes, as modulo's output changes with M.
// This approach results in O(N) keys being shuffled, which is undesirable for a
// caching use-case.
//
// Consistent hashing works by hashing all keys into a circle, which can be
// visualized as a clock. Keys are routed to a node by hashing the key, and
// searching for the first clockwise neighbor. This requires O(N) memory to
// maintain the state of the ring and log(N) time to route a key using
// binary-search, but results in O(N/M) amount of keys being shuffled when a
// node is added/removed because the addition/removal of a node results in a
// split/merge of its counter-clockwise neighbor's hash space.
//
// As an example, assume we have a ring that supports hashes from 1-12.
//
//                   12
//             11          1
//
//         10                  2
//
//        9                      3
//
//         8                   4
//
//             7           5
//                   6
//
// Add node 1 (n1). Let h(n1) = 12.
// First, we compute the hash the node, and insert it into its corresponding
// location on the ring.
//
//                   12 (n1)
//             11          1
//
//         10                  2
//
//        9                      3
//
//         8                   4
//
//             7           5
//                   6
//
// Now, to see which node a key (k1) should map to, we hash the key and search
// for its closest clockwise neighbor.
// Let h(k1) = 3. Here, we see that since n1 is the closest neighbor, as there
// are no other nodes in the ring.
//
//                   12 (n1)
//             11          1
//
//         10                  2
//
//        9                      3 (k1)
//
//         8                   4
//
//             7           5
//                   6
//
// Now, let's insert another node (n2), such that h(n2) = 6.
// Here we observe that k1 has shuffled to n2, as n2 is the closest clockwise
// neighbor to k1.
//
//                   12 (n1)
//             11          1
//
//         10                  2
//
//        9                      3 (k1)
//
//         8                   4
//
//             7           5
//                   6 (n2)
//
// Other optimizations can be made to help reduce blast radius of failures and
// the variance in keys (hot shards). One such optimization is introducing
// virtual nodes, which is to replicate a single node multiple times by salting
// it, (e.g n1-0, n1-1...).
//
// Without virtualization, failures of a node cascade as each node failing
// results in the load of the failed node being shuffled into its clockwise
// neighbor, which can result in a snowball effect across the network.
type Ring interface {
	RingReader
	ringMutator
}

// RingReader is an interface to read values from Ring.
type RingReader interface {
	// Get gets the closest clockwise node for a key in the ring.
	//
	// Each ring member is responsible for the hashes which fall in the range
	// between (myself, clockwise-neighbor].
	// This behavior is desirable so that we can re-use the return value of Get
	// to iterate around the ring (Ex. replication, retries, etc).
	//
	// Returns the node routed to and an error if we're unable to resolve a node
	// to map to.
	Get(Hashable) (Hashable, error)
}

// ringMutator defines an interface that mutates Ring.
type ringMutator interface {
	// Add adds a node to the ring.
	Add(Hashable)

	// Remove removes the node from the ring.
	//
	// Returns true if the node was removed, and false if it wasn't present to
	// begin with.
	Remove(Hashable) bool
}

// hashRing is an implementation of Ring
type hashRing struct {
	// Hashing algorithm to use when hashing keys.
	hasher hashing.Hasher

	// Replication factor for nodes; must be greater than zero.
	virtualNodes int

	lock sync.RWMutex
	ring *btree.BTree
}

// RingConfig configures settings for a Ring.
type RingConfig struct {
	// Replication factor for nodes in the ring.
	VirtualNodes int
	// Hashing implementation to use.
	Hasher hashing.Hasher
	// Degree represents the degree of the b-tree
	Degree int
}

// NewHashRing instantiates an instance of hashRing.
func NewHashRing(config RingConfig) Ring {
	return &hashRing{
		hasher:       config.Hasher,
		virtualNodes: config.VirtualNodes,
		ring:         btree.New(config.Degree),
	}
}

func (h *hashRing) Get(key Hashable) (Hashable, error) {
	h.lock.RLock()
	defer h.lock.RUnlock()

	return h.get(key)
}

func (h *hashRing) get(key Hashable) (Hashable, error) {
	// If we have no members in the ring, it's not possible to find where the
	// key belongs.
	if h.ring.Len() == 0 {
		return nil, errEmptyRing
	}

	var (
		// Compute this key's hash
		hash   = h.hasher.Hash(key.ConsistentHashKey())
		result Hashable
	)
	h.ring.AscendGreaterOrEqual(
		ringItem{
			hash:  hash,
			value: key,
		},
		func(itemIntf btree.Item) bool {
			item := itemIntf.(ringItem)
			if hash < item.hash {
				result = item.value
				return false
			}
			return true
		},
	)

	// If found nothing ascending the tree, we need to wrap around the ring to
	// the left-most (min) node.
	if result == nil {
		result = h.ring.Min().(ringItem).value
	}
	return result, nil
}

func (h *hashRing) Add(key Hashable) {
	h.lock.Lock()
	defer h.lock.Unlock()

	h.add(key)
}

func (h *hashRing) add(key Hashable) {
	// Replicate the node in the ring.
	hashKey := key.ConsistentHashKey()
	for i := 0; i < h.virtualNodes; i++ {
		virtualNode := getHashKey(hashKey, i)
		virtualNodeHash := h.hasher.Hash(virtualNode)

		// Insert it into the ring.
		h.ring.ReplaceOrInsert(ringItem{
			hash:  virtualNodeHash,
			value: key,
		})
	}
}

func (h *hashRing) Remove(key Hashable) bool {
	h.lock.Lock()
	defer h.lock.Unlock()

	return h.remove(key)
}

func (h *hashRing) remove(key Hashable) bool {
	var (
		hashKey = key.ConsistentHashKey()
		removed = false
	)

	// We need to delete all virtual nodes created for a single node.
	for i := 0; i < h.virtualNodes; i++ {
		virtualNode := getHashKey(hashKey, i)
		virtualNodeHash := h.hasher.Hash(virtualNode)
		item := ringItem{
			hash: virtualNodeHash,
		}
		if h.ring.Delete(item) != nil {
			removed = true
		}
	}
	return removed
}

// getHashKey builds a key given a base key and a virtual node number.
func getHashKey(key []byte, virtualNode int) []byte {
	return append(key, byte(virtualNode))
}

// ringItem is a helper class to represent ring nodes in the b-tree.
type ringItem struct {
	hash  uint64
	value Hashable
}

// Less implements btree.Item
func (r ringItem) Less(than btree.Item) bool {
	return r.hash < than.(ringItem).hash
}

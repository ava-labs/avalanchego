// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

// GossipTracker tracks the peers that we're currently aware of, as well as the
// peers we've told other peers about.
type GossipTracker struct {
	// a bitset of the peers that we are aware of
	local ids.BigBitSet

	// a mapping of peer => the peers we know we sent to them
	knownPeers map[ids.NodeID]ids.BigBitSet
	// a mapping of peers => the index they occupy in the bitsets
	peersToIndices map[ids.NodeID]int
	// a mapping of indices in the bitsets => the peer they correspond to
	indicesToPeers map[int]ids.NodeID

	// tail always points to an empty slot where new peers are added
	tail int
	lock sync.RWMutex
}

// NewGossipTracker returns an instance of GossipTracker
func NewGossipTracker() *GossipTracker {
	return &GossipTracker{
		local:          ids.NewBigBitSet(),
		knownPeers:     make(map[ids.NodeID]ids.BigBitSet),
		peersToIndices: make(map[ids.NodeID]int),
		indicesToPeers: make(map[int]ids.NodeID),
	}
}

// Contains returns if a peer is being tracked
func (d *GossipTracker) Contains(id ids.NodeID) bool {
	d.lock.RLock()
	defer d.lock.RUnlock()

	_, ok := d.knownPeers[id]
	return ok
}

// Add starts tracking a peer
func (d *GossipTracker) Add(id ids.NodeID) bool {
	d.lock.Lock()
	defer d.lock.Unlock()

	// Don't add the peer if it's already being tracked
	if _, ok := d.peersToIndices[id]; ok {
		return false
	}

	// add the peer
	d.peersToIndices[id] = d.tail
	d.knownPeers[id] = ids.NewBigBitSet()
	d.indicesToPeers[d.tail] = id

	d.local.Add(d.tail)

	d.tail++

	return true
}

// Remove stops tracking a given peer
func (d *GossipTracker) Remove(id ids.NodeID) bool {
	d.lock.Lock()
	defer d.lock.Unlock()

	// Only remove peers that are actually being tracked
	idx, ok := d.peersToIndices[id]
	if !ok {
		return false
	}

	evicted := d.indicesToPeers[idx]
	d.tail--

	// swap the peer-to-be-removed with the tail peer
	// if the element we're swapping with is ourselves, we can skip this swap
	// since we only need to delete instead
	if idx != d.tail {
		lastPeer := d.indicesToPeers[d.tail]

		d.indicesToPeers[idx] = lastPeer
		d.peersToIndices[lastPeer] = idx
	}

	// remove the peer from everyone else's peer lists
	for _, knownPeers := range d.knownPeers {
		// swap with the last element
		if idx != d.tail {
			knownPeers.Add(d.tail)
		}
		knownPeers.Remove(idx)
	}

	delete(d.knownPeers, evicted)
	delete(d.peersToIndices, evicted)
	delete(d.indicesToPeers, d.tail)

	d.local.Remove(d.tail)

	return true
}

// UpdateKnown adds to the peers that a peer knows about
// invariants:
// 1. [id] and [learned] should only contain nodeIDs that have been tracked with
// 	  Add(). Trying to add nodeIDs that aren't tracked yet will result in a noop
// 	  and this will return [false].
func (d *GossipTracker) UpdateKnown(id ids.NodeID, learned []ids.NodeID) bool {
	d.lock.Lock()
	defer d.lock.Unlock()

	known, ok := d.knownPeers[id]
	if !ok {
		return false
	}

	bs := ids.NewBigBitSetFromBits()
	for _, nodeID := range learned {
		idx, ok := d.peersToIndices[nodeID]
		if !ok {
			return false
		}

		bs.Add(idx)
	}

	known.Union(bs)

	return true
}

// GetUnknown returns the peers that we haven't sent to this peer
func (d *GossipTracker) GetUnknown(id ids.NodeID) (map[ids.NodeID]struct{}, bool) {
	d.lock.RLock()
	defer d.lock.RUnlock()

	// Calculate the unsent information we need to send to this peer.
	// We do this by computing the [local] information we know,
	// computing what the peer knows in its [knownPeers], and sending over
	// the difference.
	unsent := ids.NewBigBitSet()
	unsent.Union(d.local)

	knownPeers, ok := d.knownPeers[id]
	if !ok {
		return nil, false
	}

	unsent.Difference(knownPeers)

	result := make(map[ids.NodeID]struct{}, unsent.Len())

	for i := 0; i < unsent.Len(); i++ {
		if !unsent.Contains(i) {
			continue
		}

		p := d.indicesToPeers[i]
		result[p] = struct{}{}
	}

	return result, true
}

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

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
func (g *GossipTracker) Contains(id ids.NodeID) bool {
	g.lock.RLock()
	defer g.lock.RUnlock()

	_, ok := g.knownPeers[id]
	return ok
}

// Add starts tracking a peer
func (g *GossipTracker) Add(id ids.NodeID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	// Don't add the peer if it's already being tracked
	if _, ok := g.peersToIndices[id]; ok {
		return false
	}

	// add the peer
	g.peersToIndices[id] = g.tail
	g.knownPeers[id] = ids.NewBigBitSet()
	g.indicesToPeers[g.tail] = id

	g.local.Add(g.tail)

	g.tail++

	return true
}

// Remove stops tracking a given peer
func (g *GossipTracker) Remove(id ids.NodeID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	// Only remove peers that are actually being tracked
	idx, ok := g.peersToIndices[id]
	if !ok {
		return false
	}

	evicted := g.indicesToPeers[idx]
	g.tail--

	// swap the peer-to-be-removed with the tail peer
	// if the element we're swapping with is ourselves, we can skip this swap
	// since we only need to delete instead
	if idx != g.tail {
		lastPeer := g.indicesToPeers[g.tail]

		g.indicesToPeers[idx] = lastPeer
		g.peersToIndices[lastPeer] = idx
	}

	delete(g.knownPeers, evicted)
	delete(g.peersToIndices, evicted)
	delete(g.indicesToPeers, g.tail)

	g.local.Remove(g.tail)

	// remove the peer from everyone else's peer lists
	for _, knownPeers := range g.knownPeers {
		// swap the element to be removed with the tail
		if idx != g.tail {
			if knownPeers.Contains(g.tail) {
				knownPeers.Add(idx)
			} else {
				knownPeers.Remove(idx)
			}
		}
		knownPeers.Remove(g.tail)

	}

	return true
}

// UpdateKnown adds to the peers that a peer knows about
// invariants:
// 1. [id] and [learned] should only contain nodeIDs that have been tracked with
// 	  Add(). Trying to add nodeIDs that aren't tracked yet will result in a noop
// 	  and this will return [false].
func (g *GossipTracker) UpdateKnown(id ids.NodeID, learned []ids.NodeID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	known, ok := g.knownPeers[id]
	if !ok {
		return false
	}

	bs := ids.NewBigBitSetFromBits()
	for _, nodeID := range learned {
		idx, ok := g.peersToIndices[nodeID]
		if !ok {
			return false
		}

		bs.Add(idx)
	}

	known.Union(bs)

	return true
}

// GetUnknown returns the peers that we haven't sent to this peer
func (g *GossipTracker) GetUnknown(id ids.NodeID) (map[ids.NodeID]struct{}, bool) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	// Calculate the unknown information we need to send to this peer.
	// We do this by computing the [local] information we know,
	// computing what the peer knows in its [knownPeers], and sending over
	// the difference.
	unknown := ids.NewBigBitSet()
	unknown.Union(g.local)

	knownPeers, ok := g.knownPeers[id]
	if !ok {
		return nil, false
	}

	unknown.Difference(knownPeers)

	result := make(map[ids.NodeID]struct{}, unknown.Len())

	for i := 0; i < unknown.Len(); i++ {
		if !unknown.Contains(i) {
			continue
		}

		p := g.indicesToPeers[i]
		result[p] = struct{}{}
	}

	return result, true
}

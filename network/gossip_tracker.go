// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// GossipTracker tracks the peers that we're currently aware of, as well as the
// peers we've told other peers about. This data is stored in a bitset to
// optimize space, where only N (num peers) bits will be used.
//
// This is done by recording some state information of both what peers this node
// is aware of, and what peers we've told each peer about. As an example,
// say we track three peers (most-significant-bit first):
//
//	local: 		[1, 1, 1] // [p3, p2, p1] we always know about everyone
//	knownPeers:	{
//		p1: [1, 1, 1] // we have already told [p1] about all peers
//		p2: [0, 1, 1] // [p2] doesn't know about [p3]
//		p3: [0, 0, 1] // [p3] knows only about [p3]
//	}
//
// GetUnknown computes the information we haven't sent to a given peer
// (using the bitwise AND NOT operator). Ex:
//
//	GetUnknown(p1) -  [0, 0, 0]
//	GetUnknown(p2) -  [1, 0, 0]
//	GetUnknown(p3) -  [1, 1, 0]
//
// Using the gossipTracker, we can quickly compute the peers each peer doesn't
// know about using GetUnknown so that in subsequent PeerList gossip messages
// we only send information that this peer (most likely) doesn't already know
// about. The only edge-case where we'll send a redundant set of bytes is if
// another remote peer gossips to the same peer we're trying to gossip to first.
type GossipTracker interface {
	// Contains returns if a peer is being tracked
	// Returns:
	// 	[ok]: False if [id] is not tracked. True otherwise.
	Contains(id ids.NodeID) (ok bool)
	// Add starts tracking a peer
	// Returns :
	// 	[ok]: False if [id] is tracked. True otherwise.
	Add(id ids.NodeID) (ok bool)
	// Remove stops tracking a given peer
	// Returns:
	// 	[ok]: False if [id] is not tracked. True otherwise.
	Remove(id ids.NodeID) (ok bool)
	// UpdateKnown adds [learned] to the peers known by [id]
	// Returns:
	// 	[ok]: False if [id] is not tracked. True otherwise.
	UpdateKnown(id ids.NodeID, learned []ids.NodeID) (ok bool)
	// GetUnknown gets the peers that we haven't sent to this peer
	// Returns:
	// 	[unknown]: a slice of [limit] peers that [id] doesn't know about
	// 	[ok]: False if [id] is not tracked. True otherwise.
	GetUnknown(id ids.NodeID, limit int) (unknown []ids.NodeID, ok bool)
}

type gossipTracker struct {
	// the validator set
	validators validators.Manager

	// a bitset of the peers that we are aware of
	local ids.BigBitSet

	// a mapping of peer => the peers we have sent them
	knownPeers map[ids.NodeID]ids.BigBitSet
	// a mapping of peers => the index they occupy in the bitsets
	peersToIndices map[ids.NodeID]int
	// a mapping of indices in the bitsets => the peer they correspond to
	indicesToPeers map[int]ids.NodeID

	lock    sync.RWMutex
	metrics gossipTrackerMetrics
}

type GossipTrackerConfig struct {
	ValidatorManager validators.Manager
	Registerer       prometheus.Registerer
	Namespace        string
}

// NewGossipTracker returns an instance of gossipTracker
func NewGossipTracker(config GossipTrackerConfig) (GossipTracker, error) {
	m, err := newGossipTrackerMetrics(config.Registerer, fmt.Sprintf("%s_gossip_tracker", config.Namespace))
	if err != nil {
		return nil, err
	}

	return &gossipTracker{
		validators:     config.ValidatorManager,
		local:          ids.NewBigBitSet(),
		knownPeers:     make(map[ids.NodeID]ids.BigBitSet),
		peersToIndices: make(map[ids.NodeID]int),
		indicesToPeers: make(map[int]ids.NodeID),
		metrics:        m,
	}, nil
}

func (g *gossipTracker) Contains(id ids.NodeID) bool {
	g.lock.RLock()
	defer g.lock.RUnlock()

	_, ok := g.knownPeers[id]
	return ok
}

func (g *gossipTracker) Add(id ids.NodeID) (ok bool) {
	g.lock.Lock()
	defer g.lock.Unlock()

	// Don't add the peer if it's already being tracked
	if _, ok := g.peersToIndices[id]; ok {
		return false
	}

	// Add the peer to the MSB of the bitset.
	tail := len(g.peersToIndices)
	g.peersToIndices[id] = tail
	g.knownPeers[id] = ids.NewBigBitSet()
	g.indicesToPeers[tail] = id

	g.local.Add(tail)

	g.metrics.localPeersSize.Set(float64(g.local.Len()))
	g.metrics.peersToIndicesSize.Set(float64(len(g.peersToIndices)))
	g.metrics.indicesToPeersSize.Set(float64(len(g.indicesToPeers)))

	return true
}

func (g *gossipTracker) Remove(id ids.NodeID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	// Only remove peers that are actually being tracked
	idx, ok := g.peersToIndices[id]
	if !ok {
		return false
	}

	evicted := g.indicesToPeers[idx]
	// swap the peer-to-be-removed with the tail peer
	// if the element we're swapping with is ourselves, we can skip this swap
	// since we only need to delete instead
	tail := len(g.peersToIndices) - 1
	if idx != tail {
		lastPeer := g.indicesToPeers[tail]

		g.indicesToPeers[idx] = lastPeer
		g.peersToIndices[lastPeer] = idx
	}

	delete(g.knownPeers, evicted)
	delete(g.peersToIndices, evicted)
	delete(g.indicesToPeers, tail)

	g.local.Remove(tail)

	// remove the peer from everyone else's peer lists
	for _, knownPeers := range g.knownPeers {
		// swap the element to be removed with the tail
		if idx != tail {
			if knownPeers.Contains(tail) {
				knownPeers.Add(idx)
			} else {
				knownPeers.Remove(idx)
			}
		}
		knownPeers.Remove(tail)
	}

	g.metrics.localPeersSize.Set(float64(g.local.Len()))
	g.metrics.peersToIndicesSize.Set(float64(len(g.peersToIndices)))
	g.metrics.indicesToPeersSize.Set(float64(len(g.indicesToPeers)))

	return true
}

// UpdateKnown adds to the peers that a peer knows about
// invariants:
//  1. [id] and [learned] should only contain nodeIDs that have been tracked with
//     Add(). Trying to add nodeIDs that aren't tracked yet will result in a noop
//     and this will return [false].
func (g *gossipTracker) UpdateKnown(id ids.NodeID, learned []ids.NodeID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	known, ok := g.knownPeers[id]
	if !ok {
		return false
	}

	bs := ids.NewBigBitSet()
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

// GetUnknown invariant: [limit] SHOULD be > 0.
func (g *gossipTracker) GetUnknown(id ids.NodeID, limit int) ([]ids.NodeID, bool) {
	if limit <= 0 {
		return []ids.NodeID{}, false
	}

	g.lock.RLock()
	defer g.lock.RUnlock()

	// Calculate the unknown information we need to send to this peer.
	// We do this by computing the [local] information we know,
	// computing what the peer knows in its [known], and sending over
	// the difference.
	unknown := ids.NewBigBitSet()
	unknown.Union(g.local)

	known, ok := g.knownPeers[id]
	if !ok {
		return nil, false
	}

	unknown.Difference(known)

	result := make([]ids.NodeID, 0, limit)

	// We iterate from the LSB -> MSB when computing our diffs.
	// This is because [Add] always inserts at the MSB, so we retrieve the
	// unknown peers starting at the oldest unknown peer to avoid complications
	// where a subset of nodes might be "flickering" offline/online, resulting
	// in the same diff being sent over each time.
	for i := 0; i < unknown.Len(); i++ {
		// skip if the bit isn't set or if this bit isn't a validator
		if !unknown.Contains(i) || !g.validators.Contains(constants.PrimaryNetworkID, g.indicesToPeers[i]) {
			continue
		}
		// stop if we exceed the max specified elements to return
		if len(result) >= limit {
			break
		}

		result = append(result, g.indicesToPeers[i])
	}

	return result, true
}

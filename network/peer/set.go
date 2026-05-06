// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

// NoPrecondition can be supplied to [Set.Sample] to indicate that all peers
// are eligible to be returned in the sample.
func NoPrecondition(*Peer) bool {
	return true
}

// Set contains a group of peers.
type Set struct {
	peersMap   map[ids.NodeID]int // nodeID -> peer's index in peersSlice
	peersSlice []*Peer            // invariant: len(peersSlice) == len(peersMap)
}

// NewSet returns a set that does not internally manage synchronization.
//
// Only [Set.Add] and [Set.Remove] require exclusion on the data structure. The
// remaining methods are safe for concurrent use.
func NewSet() *Set {
	return &Set{
		peersMap: make(map[ids.NodeID]int),
	}
}

// Add this peer to the set.
//
// If a peer with the same [Peer.ID] is already in the set, then the new
// peer instance will replace the old peer instance.
//
// Add does not change the [Peer.ID] returned from calls to [Set.GetByIndex].
func (s *Set) Add(peer *Peer) {
	nodeID := peer.ID()
	index, ok := s.peersMap[nodeID]
	if !ok {
		s.peersMap[nodeID] = len(s.peersSlice)
		s.peersSlice = append(s.peersSlice, peer)
	} else {
		s.peersSlice[index] = peer
	}
}

// GetByID attempts to fetch a [Peer] whose [Peer.ID] is equal to nodeID.
// If no such peer exists in the set, then [false] will be returned.
func (s *Set) GetByID(nodeID ids.NodeID) (*Peer, bool) {
	index, ok := s.peersMap[nodeID]
	if !ok {
		return nil, false
	}
	return s.peersSlice[index], true
}

// GetByIndex attempts to fetch a [Peer] who has been allocated index.
// If index < 0 or index >= [Set.Len], then false will be returned.
func (s *Set) GetByIndex(index int) (*Peer, bool) {
	if index < 0 || index >= len(s.peersSlice) {
		return nil, false
	}
	return s.peersSlice[index], true
}

// Remove any [Peer] whose [Peer.ID] is equal to nodeID from the set.
func (s *Set) Remove(nodeID ids.NodeID) {
	index, ok := s.peersMap[nodeID]
	if !ok {
		return
	}

	lastIndex := len(s.peersSlice) - 1
	lastPeer := s.peersSlice[lastIndex]
	lastPeerID := lastPeer.ID()

	s.peersMap[lastPeerID] = index
	s.peersSlice[index] = lastPeer

	delete(s.peersMap, nodeID)
	s.peersSlice[lastIndex] = nil
	s.peersSlice = s.peersSlice[:lastIndex]
}

// Len returns the number of peers currently in this set.
func (s *Set) Len() int {
	return len(s.peersSlice)
}

// Sample attempts to return a random slice of peers with length n. The
// slice will not include any duplicates. Only peers that cause the
// precondition to return true will be returned in the slice.
func (s *Set) Sample(n int, precondition func(*Peer) bool) []*Peer {
	if n <= 0 {
		return nil
	}

	sampler := sampler.NewUniform()
	sampler.Initialize(uint64(len(s.peersSlice)))

	peers := make([]*Peer, 0, n)
	for len(peers) < n {
		index, hasNext := sampler.Next()
		if !hasNext {
			// We have run out of peers to attempt to sample.
			break
		}
		peer := s.peersSlice[index]
		if !precondition(peer) {
			continue
		}
		peers = append(peers, peer)
	}
	return peers
}

// AllInfo returns information about all the peers.
func (s *Set) AllInfo() []Info {
	peerInfo := make([]Info, len(s.peersSlice))
	for i, peer := range s.peersSlice {
		peerInfo[i] = peer.Info()
	}
	return peerInfo
}

// Info returns information about the requested peers if they are in the set.
func (s *Set) Info(nodeIDs []ids.NodeID) []Info {
	peerInfo := make([]Info, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if peer, ok := s.GetByID(nodeID); ok {
			peerInfo = append(peerInfo, peer.Info())
		}
	}
	return peerInfo
}

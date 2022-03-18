// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

var _ Set = &set{}

func NoPrecondition(Peer) bool { return true }

// Set contains a group of peers.
type Set interface {
	// Add this peer to the set.
	//
	// If a peer with the same [peer.ID] is already in the set, then the new
	// peer instance will replace the old peer instance.
	//
	// Add does not change the [peer.ID] returned from calls to [GetByIndex].
	Add(peer Peer)

	// GetByID attempts to fetch a [peer] whose [peer.ID] is equal to [nodeID].
	// If no such peer exists in the set, then [false] will be returned.
	GetByID(nodeID ids.ShortID) (Peer, bool)

	// GetByIndex attempts to fetch a peer who has been allocated [index]. If
	// [index] < 0 or [index] >= [Len], then false will be returned.
	GetByIndex(index int) (Peer, bool)

	// Remove any [peer] whose [peer.ID] is equal to [nodeID] from the set.
	Remove(nodeID ids.ShortID)

	// Len returns the number of peers currently in this set.
	Len() int

	// Sample attempts to return a random slice of peers with length [n]. The
	// slice will not inclide any duplicates. Only peers that cause the
	// [precondition] to return true will be returned in the slice.
	Sample(n int, precondition func(Peer) bool) []Peer

	// Returns information about all the peers.
	AllInfo() []Info

	// Info returns information about the requested peers if they are in the
	// set.
	Info(nodeIDs []ids.ShortID) []Info
}

type set struct {
	peersMap   map[ids.ShortID]int // nodeID -> peer's index in peersSlice
	peersSlice []Peer              // invariant: len(peersSlice) == len(peersMap)
}

// NewSet returns a set that does not internally manage synchronization.
//
// Only [Add] and [Remove] require exclusion on the data structure. The
// remaining methods are safe for concurrent use.
func NewSet() Set {
	return &set{
		peersMap: make(map[ids.ShortID]int),
	}
}

func (s *set) Add(peer Peer) {
	nodeID := peer.ID()
	index, ok := s.peersMap[nodeID]
	if !ok {
		s.peersMap[nodeID] = len(s.peersSlice)
		s.peersSlice = append(s.peersSlice, peer)
	} else {
		s.peersSlice[index] = peer
	}
}

func (s *set) GetByID(nodeID ids.ShortID) (Peer, bool) {
	index, ok := s.peersMap[nodeID]
	if !ok {
		return nil, false
	}
	return s.peersSlice[index], true
}

func (s *set) GetByIndex(index int) (Peer, bool) {
	if index < 0 || index >= len(s.peersSlice) {
		return nil, false
	}
	return s.peersSlice[index], true
}

func (s *set) Remove(nodeID ids.ShortID) {
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

func (s *set) Len() int {
	return len(s.peersSlice)
}

func (s *set) Sample(n int, precondition func(Peer) bool) []Peer {
	if n <= 0 {
		return nil
	}

	sampler := sampler.NewUniform()
	// It is impossible for the sampler to report an error here. Since
	// [len(s.peersSlice)] <= MaxInt64.
	_ = sampler.Initialize(uint64(len(s.peersSlice)))

	peers := make([]Peer, 0, n)
	for len(peers) < n {
		index, err := sampler.Next()
		if err != nil {
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

func (s *set) AllInfo() []Info {
	peerInfo := make([]Info, len(s.peersSlice))
	for i, peer := range s.peersSlice {
		peerInfo[i] = peer.Info()
	}
	return peerInfo
}

func (s *set) Info(nodeIDs []ids.ShortID) []Info {
	peerInfo := make([]Info, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if peer, ok := s.GetByID(nodeID); ok {
			peerInfo = append(peerInfo, peer.Info())
		}
	}
	return peerInfo
}

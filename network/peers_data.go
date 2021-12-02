// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

// peersData encapsulate all peers known to Network.
// peers in peersData can be retrieved by their id and can be iterated over
// via getList() method, returning a slice of peers.
// Index associated with peers MAY CHANGE following removal of other peers

type peersData struct {
	peersIdxes map[ids.ShortID]int // peerID -> *peer index in peersList
	peersList  []*peer             // invariant: len(peersList) == len(peersIdxes)
}

func (p *peersData) initialize() {
	p.peersIdxes = make(map[ids.ShortID]int)
	p.peersList = make([]*peer, 0)
}

func (p *peersData) reset() {
	p.initialize()
}

func (p *peersData) add(peer *peer) {
	if _, ok := p.getByID(peer.nodeID); !ok { // new insertion
		p.peersList = append(p.peersList, peer)
		p.peersIdxes[peer.nodeID] = len(p.peersList) - 1
	} else { // update
		p.peersList[p.peersIdxes[peer.nodeID]] = peer
	}
}

func (p *peersData) remove(peer *peer) {
	if _, ok := p.getByID(peer.nodeID); !ok {
		return
	}

	// Drop p by replacing it with last peer in peersList.
	// if p is already the last peer, simply drop it.
	// Keep peersIdxes synced. peersList order is not preserved
	idToDrop := peer.nodeID
	idxToReplace := p.peersIdxes[idToDrop]

	if idxToReplace != len(p.peersList)-1 {
		lastPeer := p.peersList[len(p.peersList)-1]
		p.peersList[idxToReplace] = lastPeer
		p.peersIdxes[lastPeer.nodeID] = idxToReplace
	}
	p.peersList = p.peersList[:len(p.peersList)-1]
	delete(p.peersIdxes, idToDrop)
}

func (p *peersData) getByID(id ids.ShortID) (*peer, bool) {
	if idx, ok := p.peersIdxes[id]; ok {
		return p.peersList[idx], ok
	}
	return nil, false
}

func (p *peersData) getByIdx(idx int) (*peer, bool) {
	// peer index may change following other peers removal
	// since upon removal order is not guaranteed.
	if idx < 0 || idx >= len(p.peersList) {
		return nil, false
	}
	return p.peersList[idx], true
}

func (p *peersData) size() int {
	return len(p.peersList)
}

// Randomly sample [n] peers that have finished the handshake and tracks the subnetID.
// If < [n] peers have finished the handshake and tracks the subnetID, returns < [n] peers.
// If [n] > [p.size()], returns <= [p.size()] peers.
// [n] must be >= 0.
// [p] must not be modified while this method is executing.
func (p *peersData) sample(subnetID ids.ID, validatorOnly bool, n int) ([]*peer, error) {
	numPeers := p.size()
	if numPeers < n {
		n = numPeers
	}
	if n == 0 {
		return nil, nil
	}
	s := sampler.NewUniform()
	if err := s.Initialize(uint64(numPeers)); err != nil {
		return nil, err
	}
	peers := make([]*peer, 0, n) // peers we'll gossip to
	for len(peers) < n {
		idx, err := s.Next()
		if err != nil {
			// all peers have been sampled and not enough valid ones found.
			// return what we have
			return peers, nil
		}
		peer := p.peersList[idx]
		if !peer.finishedHandshake.GetValue() || !peer.trackedSubnets.Contains(subnetID) || (validatorOnly && !peer.net.config.Validators.Contains(subnetID, peer.nodeID)) {
			continue
		}
		peers = append(peers, peer)
	}
	return peers, nil
}

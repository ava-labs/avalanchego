package network

import "github.com/ava-labs/avalanchego/ids"

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
	p.peersIdxes = make(map[ids.ShortID]int)
	p.peersList = make([]*peer, 0)
}

func (p *peersData) add(peer *peer) {
	if _, ok := p.getByID(peer.id); !ok { // new insertion
		p.peersList = append(p.peersList, peer)
		p.peersIdxes[peer.id] = len(p.peersList) - 1
	} else { // update
		p.peersList[p.peersIdxes[peer.id]] = peer
	}
}

func (p *peersData) remove(peer *peer) {
	if _, ok := p.getByID(peer.id); !ok {
		return
	}

	// Drop p by replacing it with last peer in peersList.
	// if p is already the last peer, simply drop it.
	// Keep peersIdxes synced. peersList order is not preserved
	idToDrop := peer.id
	idxToReplace := p.peersIdxes[idToDrop]

	if idxToReplace != len(p.peersList)-1 {
		p.peersList[idxToReplace] = p.peersList[len(p.peersList)-1]
		p.peersIdxes[p.peersList[idxToReplace].id] = idxToReplace
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

// exposed for to allow iteration
func (p *peersData) getList() []*peer {
	return p.peersList
}

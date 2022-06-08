// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"math"
	"math/rand"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	utils_math "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ethereum/go-ethereum/log"
)

const (
	bandwidthHalflife = 1 * time.Minute

	// controls how eagerly we connect to new peers vs. using
	// peers with known good response bandwidth.
	desiredMinTrackedPeers = 10
	newPeerConnectFactor   = 0.1
)

// information we track on a given peer
type peerInfo struct {
	version   version.Application
	bandwidth utils_math.Averager
}

// peerTracker tracks the bandwidth of responses coming from peers,
// preferring to contact peers with known good bandwidth, connecting
// to new peers with an exponentially decaying probability.
// Note: is not thread safe, caller must handle synchronization.
type peerTracker struct {
	peers         map[ids.NodeID]peerInfo // all peers we are connected to
	trackedPeers  ids.NodeIDSet           // peers that we have sent a request to
	bandwidthHeap utils_math.AveragerHeap // tracks bandwidth peers are responding with
}

func NewPeerTracker() *peerTracker {
	return &peerTracker{
		peers: make(map[ids.NodeID]peerInfo),
		// TODO: use a gauge to record the size of [trackedPeers]
		trackedPeers:  make(ids.NodeIDSet),
		bandwidthHeap: utils_math.NewMaxAveragerHeap(),
	}
}

// shouldTrackNewPeer returns true if we are not connected to enough peers.
// otherwise returns true probabilisitically based on the number of tracked peers.
func (p *peerTracker) shouldTrackNewPeer() bool {
	numTrackedPeers := len(p.trackedPeers)
	if numTrackedPeers < desiredMinTrackedPeers {
		return true
	}
	if numTrackedPeers >= len(p.peers) {
		// already tracking all the peers
		return false
	}
	if _, averager, ok := p.bandwidthHeap.Peek(); ok && averager.Read() == 0 {
		// if the best peer has a capacity of 0, we should try to add another peer
		return true
	}
	newPeerProbability := math.Exp(-float64(numTrackedPeers) * newPeerConnectFactor)
	return rand.Float64() < newPeerProbability
}

func (p *peerTracker) GetAnyPeer(minVersion version.Application) (ids.NodeID, bool) {
	if p.shouldTrackNewPeer() {
		for nodeID := range p.peers {
			// if minVersion is specified and peer's verion is less, skip
			if minVersion != nil && p.peers[nodeID].version.Compare(minVersion) < 0 {
				continue
			}
			// skip peers already tracked
			if p.trackedPeers.Contains(nodeID) {
				continue
			}
			log.Debug("peer tracking: connecting to new peer", "trackedPeers", len(p.trackedPeers), "nodeID", nodeID)
			return nodeID, true
		}
	}
	nodeID, averager, ok := p.bandwidthHeap.Pop()
	if ok {
		log.Debug("peer tracking: popping peer", "nodeID", nodeID, "bandwidth", averager.Read())
		return nodeID, true
	}
	// if no nodes found in the bandwidth heap, return a tracked node at random
	return p.trackedPeers.Peek()
}

func (p *peerTracker) TrackPeer(nodeID ids.NodeID) {
	p.trackedPeers.Add(nodeID)
}

func (p *peerTracker) TrackBandwidth(nodeID ids.NodeID, bandwidth float64) {
	peer, exists := p.peers[nodeID]
	if !exists {
		// we're not connected to this peer, nothing to do here
		log.Debug("tracking bandwidth for untracked peer", "nodeID", nodeID)
		return
	}

	if peer.bandwidth == nil {
		peer.bandwidth = utils_math.NewAverager(bandwidth, bandwidthHalflife, time.Now())
	} else {
		peer.bandwidth.Observe(bandwidth, time.Now())
	}
	p.bandwidthHeap.Add(nodeID, peer.bandwidth)
}

// Connected should be called when [nodeID] connects to this node
func (p *peerTracker) Connected(nodeID ids.NodeID, nodeVersion version.Application) {
	if peer, exists := p.peers[nodeID]; exists {
		// Peer is already connected, update the version if it has changed.
		// Log a warning message since the consensus engine should never call Connected on a peer
		// that we have already marked as Connected.
		if nodeVersion.Compare(peer.version) != 0 {
			p.peers[nodeID] = peerInfo{
				version:   nodeVersion,
				bandwidth: peer.bandwidth,
			}
			log.Warn("updating node version of already connected peer", "nodeID", nodeID, "storedVersion", peer.version, "nodeVersion", nodeVersion)
		} else {
			log.Warn("ignoring peer connected event for already connected peer with identical version", "nodeID", nodeID)
		}
		return
	}

	p.peers[nodeID] = peerInfo{
		version: nodeVersion,
	}
}

// Disconnected should be called when [nodeID] disconnects from this node
func (p *peerTracker) Disconnected(nodeID ids.NodeID) {
	_, _ = p.bandwidthHeap.Remove(nodeID)
	p.trackedPeers.Remove(nodeID)
	delete(p.peers, nodeID)
}

// Size returns the number of peers the node is connected to
func (p *peerTracker) Size() int {
	return len(p.peers)
}

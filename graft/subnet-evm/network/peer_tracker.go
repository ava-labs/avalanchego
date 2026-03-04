// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"math"
	"time"

	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/metrics"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils/rand"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	bandwidthHalflife = 5 * time.Minute

	// controls how eagerly we connect to new peers vs. using
	// peers with known good response bandwidth.
	desiredMinResponsivePeers = 20
	newPeerConnectFactor      = 0.1

	// controls how often we prefer a random responsive peer over the most
	// performant peer.
	randomPeerProbability = 0.2
)

// information we track on a given peer
type peerInfo struct {
	version   *version.Application
	bandwidth safemath.Averager
}

// peerTracker tracks the bandwidth of responses coming from peers,
// preferring to contact peers with known good bandwidth, connecting
// to new peers with an exponentially decaying probability.
// Note: is not thread safe, caller must handle synchronization.
type peerTracker struct {
	peers                  map[ids.NodeID]*peerInfo // all peers we are connected to
	numTrackedPeers        metrics.Gauge
	trackedPeers           set.Set[ids.NodeID] // peers that we have sent a request to
	numResponsivePeers     metrics.Gauge
	responsivePeers        set.Set[ids.NodeID]   // peers that responded to the last request they were sent
	bandwidthHeap          safemath.AveragerHeap // tracks bandwidth peers are responding with
	averageBandwidthMetric metrics.GaugeFloat64
	averageBandwidth       safemath.Averager
}

func NewPeerTracker() *peerTracker {
	return &peerTracker{
		peers:                  make(map[ids.NodeID]*peerInfo),
		numTrackedPeers:        metrics.GetOrRegisterGauge("net_tracked_peers", nil),
		trackedPeers:           make(set.Set[ids.NodeID]),
		numResponsivePeers:     metrics.GetOrRegisterGauge("net_responsive_peers", nil),
		responsivePeers:        make(set.Set[ids.NodeID]),
		bandwidthHeap:          safemath.NewMaxAveragerHeap(),
		averageBandwidthMetric: metrics.GetOrRegisterGaugeFloat64("net_average_bandwidth", nil),
		averageBandwidth:       safemath.NewAverager(0, bandwidthHalflife, time.Now()),
	}
}

// shouldTrackNewPeer returns true if we are not connected to enough peers.
// otherwise returns true probabilistically based on the number of tracked peers.
func (p *peerTracker) shouldTrackNewPeer() (bool, error) {
	numResponsivePeers := p.responsivePeers.Len()
	if numResponsivePeers < desiredMinResponsivePeers {
		return true, nil
	}
	if len(p.trackedPeers) >= len(p.peers) {
		// already tracking all the peers
		return false, nil
	}
	newPeerProbability := math.Exp(-float64(numResponsivePeers) * newPeerConnectFactor)
	randomValue, err := rand.SecureFloat64()
	if err != nil {
		return false, err
	}
	return randomValue < newPeerProbability, nil
}

// getResponsivePeer returns a random [ids.NodeID] of a peer that has responded
// to a request.
func (p *peerTracker) getResponsivePeer() (ids.NodeID, safemath.Averager, bool) {
	nodeID, ok := p.responsivePeers.Peek()
	if !ok {
		return ids.NodeID{}, nil, false
	}
	averager, ok := p.bandwidthHeap.Remove(nodeID)
	if ok {
		return nodeID, averager, true
	}
	peer := p.peers[nodeID]
	return nodeID, peer.bandwidth, true
}

func (p *peerTracker) GetAnyPeer(minVersion *version.Application) (ids.NodeID, bool, error) {
	shouldTrackNewPeer, err := p.shouldTrackNewPeer()
	if err != nil {
		return ids.NodeID{}, false, err
	}
	if shouldTrackNewPeer {
		for nodeID := range p.peers {
			// if minVersion is specified and peer's version is less, skip
			if minVersion != nil && p.peers[nodeID].version.Compare(minVersion) < 0 {
				continue
			}
			// skip peers already tracked
			if p.trackedPeers.Contains(nodeID) {
				continue
			}
			log.Debug("peer tracking: connecting to new peer", "trackedPeers", len(p.trackedPeers), "nodeID", nodeID)
			return nodeID, true, nil
		}
	}
	var (
		nodeID   ids.NodeID
		ok       bool
		random   bool
		averager safemath.Averager
	)
	randomValue, err := rand.SecureFloat64()
	switch {
	case err != nil:
		return ids.NodeID{}, false, err
	case randomValue < randomPeerProbability:
		random = true
		nodeID, averager, ok = p.getResponsivePeer()
	default:
		nodeID, averager, ok = p.bandwidthHeap.Pop()
	}
	if ok {
		log.Debug("peer tracking: popping peer", "nodeID", nodeID, "bandwidth", averager.Read(), "random", random)
		return nodeID, true, err
	}
	// if no nodes found in the bandwidth heap, return a tracked node at random
	nodeID, ok = p.trackedPeers.Peek()
	return nodeID, ok, nil
}

func (p *peerTracker) TrackPeer(nodeID ids.NodeID) {
	p.trackedPeers.Add(nodeID)
	p.numTrackedPeers.Update(int64(p.trackedPeers.Len()))
}

func (p *peerTracker) TrackBandwidth(nodeID ids.NodeID, bandwidth float64) {
	peer := p.peers[nodeID]
	if peer == nil {
		// we're not connected to this peer, nothing to do here
		log.Debug("tracking bandwidth for untracked peer", "nodeID", nodeID)
		return
	}

	now := time.Now()
	if peer.bandwidth == nil {
		peer.bandwidth = safemath.NewAverager(bandwidth, bandwidthHalflife, now)
	} else {
		peer.bandwidth.Observe(bandwidth, now)
	}
	p.bandwidthHeap.Add(nodeID, peer.bandwidth)

	if bandwidth == 0 {
		p.responsivePeers.Remove(nodeID)
	} else {
		p.responsivePeers.Add(nodeID)
		p.averageBandwidth.Observe(bandwidth, now)
		p.averageBandwidthMetric.Update(p.averageBandwidth.Read())
	}
	p.numResponsivePeers.Update(int64(p.responsivePeers.Len()))
}

// Connected should be called when [nodeID] connects to this node
func (p *peerTracker) Connected(nodeID ids.NodeID, nodeVersion *version.Application) {
	if peer := p.peers[nodeID]; peer != nil {
		// Peer is already connected, update the version if it has changed.
		// Log a warning message since the consensus engine should never call Connected on a peer
		// that we have already marked as Connected.
		if nodeVersion.Compare(peer.version) != 0 {
			p.peers[nodeID] = &peerInfo{
				version:   nodeVersion,
				bandwidth: peer.bandwidth,
			}
			log.Warn("updating node version of already connected peer", "nodeID", nodeID, "storedVersion", peer.version, "nodeVersion", nodeVersion)
		} else {
			log.Warn("ignoring peer connected event for already connected peer with identical version", "nodeID", nodeID)
		}
		return
	}

	p.peers[nodeID] = &peerInfo{
		version: nodeVersion,
	}
}

// Disconnected should be called when [nodeID] disconnects from this node
func (p *peerTracker) Disconnected(nodeID ids.NodeID) {
	p.bandwidthHeap.Remove(nodeID)
	p.trackedPeers.Remove(nodeID)
	p.numTrackedPeers.Update(int64(p.trackedPeers.Len()))
	p.responsivePeers.Remove(nodeID)
	p.numResponsivePeers.Update(int64(p.responsivePeers.Len()))
	delete(p.peers, nodeID)
}

// Size returns the number of peers the node is connected to
func (p *peerTracker) Size() int {
	return len(p.peers)
}

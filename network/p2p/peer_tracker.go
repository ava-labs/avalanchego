// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	bandwidthHalflife = 5 * time.Minute

	// controls how eagerly we connect to new peers vs. using peers with known
	// good response bandwidth.
	desiredMinResponsivePeers = 20
	newPeerConnectFactor      = 0.1

	// The probability that, when we select a peer, we select randomly rather
	// than based on their performance.
	randomPeerProbability = 0.2
)

var errDuplicatePeer = errors.New("connected to duplicate peer")

// information we track on a given peer
type peerInfo struct {
	bandwidth safemath.Averager
}

// Tracks the bandwidth of responses coming from peers,
// preferring to contact peers with known good bandwidth, connecting
// to new peers with an exponentially decaying probability.
type PeerTracker struct {
	// Lock to protect concurrent access to the peer tracker
	lock sync.RWMutex
	// All peers we are connected to
	peers map[ids.NodeID]*peerInfo
	// Peers that we're connected to that we haven't sent a request to since we
	// most recently connected to them.
	untrackedPeers set.Set[ids.NodeID]
	// Peers that we're connected to that we've sent a request to since we most
	// recently connected to them.
	trackedPeers set.Set[ids.NodeID]
	// Peers that we're connected to that responded to the last request they
	// were sent.
	responsivePeers set.Set[ids.NodeID]
	// Max heap that contains the average bandwidth of peers.
	bandwidthHeap heap.Map[ids.NodeID, safemath.Averager]
	// average bandwidth is only used for metrics
	averageBandwidth safemath.Averager

	log                    logging.Logger
	minVersion             *version.Application
	numTrackedPeers        prometheus.Gauge
	numResponsivePeers     prometheus.Gauge
	averageBandwidthMetric prometheus.Gauge
}

func NewPeerTracker(
	log logging.Logger,
	metricsNamespace string,
	registerer prometheus.Registerer,
	minVersion *version.Application,
) (*PeerTracker, error) {
	t := &PeerTracker{
		peers:           make(map[ids.NodeID]*peerInfo),
		trackedPeers:    make(set.Set[ids.NodeID]),
		responsivePeers: make(set.Set[ids.NodeID]),
		bandwidthHeap: heap.NewMap[ids.NodeID, safemath.Averager](func(a, b safemath.Averager) bool {
			return a.Read() > b.Read()
		}),
		averageBandwidth: safemath.NewAverager(0, bandwidthHalflife, time.Now()),
		log:              log,
		minVersion:       minVersion,
		numTrackedPeers: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Name:      "num_tracked_peers",
				Help:      "number of tracked peers",
			},
		),
		numResponsivePeers: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Name:      "num_responsive_peers",
				Help:      "number of responsive peers",
			},
		),
		averageBandwidthMetric: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Name:      "average_bandwidth",
				Help:      "average sync bandwidth used by peers",
			},
		),
	}

	err := utils.Err(
		registerer.Register(t.numTrackedPeers),
		registerer.Register(t.numResponsivePeers),
		registerer.Register(t.averageBandwidthMetric),
	)
	return t, err
}

// Returns true if:
//   - We have not observed the desired minimum number of responsive peers.
//   - Randomly with the freqeuency decreasing as the number of tracked peers
//     increases.
//
// Assumes the read lock is held.
func (p *PeerTracker) shouldTrackNewPeer() bool {
	numResponsivePeers := p.responsivePeers.Len()
	if numResponsivePeers < desiredMinResponsivePeers {
		return true
	}
	if p.trackedPeers.Len() >= len(p.peers) {
		return false // already tracking all peers
	}

	// TODO danlaine: we should consider tuning this probability function.
	// With [newPeerConnectFactor] as 0.1 the probabilities are:
	//
	// numResponsivePeers | probability
	// 100                | 4.5399929762484854e-05
	// 200                | 2.061153622438558e-09
	// 500                | 1.9287498479639178e-22
	// 1000               | 3.720075976020836e-44
	// 2000               | 1.3838965267367376e-87
	// 5000               | 7.124576406741286e-218
	//
	// In other words, the probability drops off extremely quickly.
	newPeerProbability := math.Exp(-float64(numResponsivePeers) * newPeerConnectFactor)
	return rand.Float64() < newPeerProbability // #nosec G404
}

// SelectPeer that we could send a request to.
// If we should track more peers, returns a random peer with version >= [minVersion], if any exist.
// Otherwise, with probability [randomPeerProbability] returns a random peer from [p.responsivePeers].
// With probability [1-randomPeerProbability] returns the peer in [p.bandwidthHeap] with the highest bandwidth.
func (p *PeerTracker) SelectPeer() (ids.NodeID, bool) {
	p.lock.RLock()
	defer p.lock.RUnlock()

	if p.shouldTrackNewPeer() {
		if nodeID, ok := p.untrackedPeers.Peek(); ok {
			p.log.Debug("tracking peer",
				zap.Int("trackedPeers", p.trackedPeers.Len()),
				zap.Stringer("nodeID", nodeID),
			)
			return nodeID, true
		}
	}

	var (
		nodeID ids.NodeID
		ok     bool
	)
	useRand := rand.Float64() < randomPeerProbability // #nosec G404
	if useRand {
		nodeID, ok = p.responsivePeers.Peek()
	} else {
		nodeID, _, ok = p.bandwidthHeap.Peek()
	}
	if !ok {
		// if no nodes found in the bandwidth heap, return a tracked node at random
		return p.trackedPeers.Peek()
	}
	p.log.Debug(
		"peer tracking: popping peer",
		zap.Stringer("nodeID", nodeID),
		zap.Bool("random", useRand),
	)
	return nodeID, true
}

// Record that we sent a request to [nodeID].
func (p *PeerTracker) RegisterRequest(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.untrackedPeers.Remove(nodeID)
	p.trackedPeers.Add(nodeID)
	p.bandwidthHeap.Remove(nodeID)

	p.numTrackedPeers.Set(float64(p.trackedPeers.Len()))
}

// Record that we observed that [nodeID]'s bandwidth is [bandwidth].
// Adds the peer's bandwidth averager to the bandwidth heap.
func (p *PeerTracker) RegisterResponse(nodeID ids.NodeID, bandwidth float64) {
	p.updateBandwidth(nodeID, bandwidth, true)
}

// Record that a request failed to [nodeID].
// Adds the peer's bandwidth averager to the bandwidth heap.
func (p *PeerTracker) RegisterFailure(nodeID ids.NodeID) {
	p.updateBandwidth(nodeID, 0, false)
}

func (p *PeerTracker) updateBandwidth(nodeID ids.NodeID, bandwidth float64, responsive bool) {
	p.lock.Lock()
	defer p.lock.Unlock()

	peer, ok := p.peers[nodeID]
	if !ok {
		// we're not connected to this peer, nothing to do here
		p.log.Debug("tracking bandwidth for untracked peer",
			zap.Stringer("nodeID", nodeID),
		)
		return
	}

	now := time.Now()
	if peer.bandwidth == nil {
		peer.bandwidth = safemath.NewAverager(bandwidth, bandwidthHalflife, now)
	} else {
		peer.bandwidth.Observe(bandwidth, now)
	}
	p.bandwidthHeap.Push(nodeID, peer.bandwidth)
	p.averageBandwidth.Observe(bandwidth, now)

	if responsive {
		p.responsivePeers.Add(nodeID)
	} else {
		p.responsivePeers.Remove(nodeID)
	}

	p.numResponsivePeers.Set(float64(p.responsivePeers.Len()))
	p.averageBandwidthMetric.Set(p.averageBandwidth.Read())
}

// Connected should be called when [nodeID] connects to this node
func (p *PeerTracker) Connected(nodeID ids.NodeID, nodeVersion *version.Application) error {
	// if minVersion is specified and peer's version is less, skip
	if p.minVersion != nil && nodeVersion.Compare(p.minVersion) < 0 {
		return nil
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	if _, ok := p.peers[nodeID]; ok {
		return errDuplicatePeer
	}

	p.peers[nodeID] = &peerInfo{}
	p.untrackedPeers.Add(nodeID)
	return nil
}

// Disconnected should be called when [nodeID] disconnects from this node
func (p *PeerTracker) Disconnected(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	delete(p.peers, nodeID)
	p.untrackedPeers.Remove(nodeID)
	p.trackedPeers.Remove(nodeID)
	p.responsivePeers.Remove(nodeID)
	p.bandwidthHeap.Remove(nodeID)

	p.numTrackedPeers.Set(float64(p.trackedPeers.Len()))
	p.numResponsivePeers.Set(float64(p.responsivePeers.Len()))
}

// Returns the number of queriable peers the node is connected to.
func (p *PeerTracker) Size() int {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return len(p.peers)
}

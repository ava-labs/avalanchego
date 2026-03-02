// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

// Tracks the bandwidth of responses coming from peers,
// preferring to contact peers with known good bandwidth, connecting
// to new peers with an exponentially decaying probability.
type PeerTracker struct {
	// Lock to protect concurrent access to the peer tracker
	lock sync.RWMutex
	// Peers that we're connected to that we haven't sent a request to since we
	// most recently connected to them.
	untrackedPeers set.Set[ids.NodeID]
	// Peers that we're connected to that we've sent a request to since we most
	// recently connected to them.
	trackedPeers set.Set[ids.NodeID]
	// Peers that we're connected to that responded to the last request they
	// were sent.
	responsivePeers set.Set[ids.NodeID]
	// Bandwidth of peers that we have measured.
	peerBandwidth map[ids.NodeID]safemath.Averager
	// Max heap that contains the average bandwidth of peers that do not have an
	// outstanding request.
	bandwidthHeap heap.Map[ids.NodeID, safemath.Averager]
	// Average bandwidth is only used for metrics.
	averageBandwidth safemath.Averager

	// The below fields are assumed to be constant and are not protected by the
	// lock.
	log          logging.Logger
	ignoredNodes set.Set[ids.NodeID]
	minVersion   *version.Application
	metrics      peerTrackerMetrics
}

type peerTrackerMetrics struct {
	numTrackedPeers    prometheus.Gauge
	numResponsivePeers prometheus.Gauge
	averageBandwidth   prometheus.Gauge
}

func NewPeerTracker(
	log logging.Logger,
	metricsNamespace string,
	registerer prometheus.Registerer,
	ignoredNodes set.Set[ids.NodeID],
	minVersion *version.Application,
) (*PeerTracker, error) {
	t := &PeerTracker{
		peerBandwidth: make(map[ids.NodeID]safemath.Averager),
		bandwidthHeap: heap.NewMap[ids.NodeID, safemath.Averager](func(a, b safemath.Averager) bool {
			return a.Read() > b.Read()
		}),
		averageBandwidth: safemath.NewAverager(0, bandwidthHalflife, time.Now()),
		log:              log,
		ignoredNodes:     ignoredNodes,
		minVersion:       minVersion,
		metrics: peerTrackerMetrics{
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
			averageBandwidth: prometheus.NewGauge(
				prometheus.GaugeOpts{
					Namespace: metricsNamespace,
					Name:      "average_bandwidth",
					Help:      "average sync bandwidth used by peers",
				},
			),
		},
	}

	err := errors.Join(
		registerer.Register(t.metrics.numTrackedPeers),
		registerer.Register(t.metrics.numResponsivePeers),
		registerer.Register(t.metrics.averageBandwidth),
	)
	return t, err
}

// Returns true if:
//   - We have not observed the desired minimum number of responsive peers.
//   - Randomly with the frequency decreasing as the number of responsive peers
//     increases.
//
// Assumes the read lock is held.
func (p *PeerTracker) shouldSelectUntrackedPeer() bool {
	numResponsivePeers := p.responsivePeers.Len()
	if numResponsivePeers < desiredMinResponsivePeers {
		return true
	}
	if p.untrackedPeers.Len() == 0 {
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
//
// If we should track more peers, returns a random untracked peer, if any exist.
// Otherwise, with probability [randomPeerProbability] returns a random peer
// from [p.responsivePeers].
// With probability [1-randomPeerProbability] returns the peer in
// [p.bandwidthHeap] with the highest bandwidth.
//
// Returns false if there are no connected peers.
func (p *PeerTracker) SelectPeer() (ids.NodeID, bool) {
	p.lock.RLock()
	defer p.lock.RUnlock()

	if p.shouldSelectUntrackedPeer() {
		if nodeID, ok := p.untrackedPeers.Peek(); ok {
			p.log.Debug("selecting peer",
				zap.String("reason", "untracked"),
				zap.Stringer("nodeID", nodeID),
				zap.Int("trackedPeers", p.trackedPeers.Len()),
				zap.Int("responsivePeers", p.responsivePeers.Len()),
			)
			return nodeID, true
		}
	}

	useBandwidthHeap := rand.Float64() > randomPeerProbability // #nosec G404
	if useBandwidthHeap {
		if nodeID, bandwidth, ok := p.bandwidthHeap.Peek(); ok {
			p.log.Debug("selecting peer",
				zap.String("reason", "bandwidth"),
				zap.Stringer("nodeID", nodeID),
				zap.Float64("bandwidth", bandwidth.Read()),
			)
			return nodeID, true
		}
	} else {
		if nodeID, ok := p.responsivePeers.Peek(); ok {
			p.log.Debug("selecting peer",
				zap.String("reason", "responsive"),
				zap.Stringer("nodeID", nodeID),
			)
			return nodeID, true
		}
	}

	if nodeID, ok := p.trackedPeers.Peek(); ok {
		p.log.Debug("selecting peer",
			zap.String("reason", "tracked"),
			zap.Stringer("nodeID", nodeID),
			zap.Bool("checkedBandwidthHeap", useBandwidthHeap),
		)
		return nodeID, true
	}

	// We're not connected to any peers.
	return ids.EmptyNodeID, false
}

// Record that we sent a request to [nodeID].
//
// Removes the peer's bandwidth averager from the bandwidth heap.
func (p *PeerTracker) RegisterRequest(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.untrackedPeers.Remove(nodeID)
	p.trackedPeers.Add(nodeID)
	p.bandwidthHeap.Remove(nodeID)

	p.metrics.numTrackedPeers.Set(float64(p.trackedPeers.Len()))
}

// Record that we observed that [nodeID]'s bandwidth is [bandwidth].
//
// Adds the peer's bandwidth averager to the bandwidth heap.
func (p *PeerTracker) RegisterResponse(nodeID ids.NodeID, bandwidth float64) {
	p.updateBandwidth(nodeID, bandwidth, true)
}

// Record that a request failed to [nodeID].
//
// Adds the peer's bandwidth averager to the bandwidth heap.
func (p *PeerTracker) RegisterFailure(nodeID ids.NodeID) {
	p.updateBandwidth(nodeID, 0, false)
}

func (p *PeerTracker) updateBandwidth(nodeID ids.NodeID, bandwidth float64, responsive bool) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if !p.trackedPeers.Contains(nodeID) {
		// we're not tracking this peer, nothing to do here
		p.log.Debug("tracking bandwidth for untracked peer",
			zap.Stringer("nodeID", nodeID),
		)
		return
	}

	now := time.Now()
	peerBandwidth, ok := p.peerBandwidth[nodeID]
	if ok {
		peerBandwidth.Observe(bandwidth, now)
	} else {
		peerBandwidth = safemath.NewAverager(bandwidth, bandwidthHalflife, now)
		p.peerBandwidth[nodeID] = peerBandwidth
	}
	p.bandwidthHeap.Push(nodeID, peerBandwidth)
	p.averageBandwidth.Observe(bandwidth, now)

	if responsive {
		p.responsivePeers.Add(nodeID)
	} else {
		p.responsivePeers.Remove(nodeID)
	}

	p.metrics.numResponsivePeers.Set(float64(p.responsivePeers.Len()))
	p.metrics.averageBandwidth.Set(p.averageBandwidth.Read())
}

// Connected should be called when [nodeID] connects to this node.
func (p *PeerTracker) Connected(nodeID ids.NodeID, nodeVersion *version.Application) {
	// If this peer should be ignored, don't mark it as connected.
	if p.ignoredNodes.Contains(nodeID) {
		return
	}
	// If minVersion is specified and peer's version is less, don't mark it as
	// connected.
	if p.minVersion != nil && nodeVersion.Compare(p.minVersion) < 0 {
		return
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	p.untrackedPeers.Add(nodeID)
}

// Disconnected should be called when [nodeID] disconnects from this node.
func (p *PeerTracker) Disconnected(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	// Because of the checks performed in Connected, it's possible that this
	// node was never marked as connected here. However, all of the below
	// functions are noops if called with a peer that was never marked as
	// connected.
	p.untrackedPeers.Remove(nodeID)
	p.trackedPeers.Remove(nodeID)
	p.responsivePeers.Remove(nodeID)
	delete(p.peerBandwidth, nodeID)
	p.bandwidthHeap.Remove(nodeID)

	p.metrics.numTrackedPeers.Set(float64(p.trackedPeers.Len()))
	p.metrics.numResponsivePeers.Set(float64(p.responsivePeers.Len()))
}

// Returns the number of peers the node is connected to.
func (p *PeerTracker) Size() int {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.untrackedPeers.Len() + p.trackedPeers.Len()
}

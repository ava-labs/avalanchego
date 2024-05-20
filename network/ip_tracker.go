// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto/rand"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	saltSize                       = 32
	minCountEstimate               = 128
	targetFalsePositiveProbability = .001
	maxFalsePositiveProbability    = .01
	// By setting maxIPEntriesPerNode > 1, we allow nodes to update their IP at
	// least once per bloom filter reset.
	maxIPEntriesPerNode = 2

	untrackedTimestamp = -2
	olderTimestamp     = -1
	sameTimestamp      = 0
	newerTimestamp     = 1
	newTimestamp       = 2
)

var _ validators.SetCallbackListener = (*ipTracker)(nil)

func newIPTracker(
	log logging.Logger,
	namespace string,
	registerer prometheus.Registerer,
) (*ipTracker, error) {
	bloomNamespace := metric.AppendNamespace(namespace, "ip_bloom")
	bloomMetrics, err := bloom.NewMetrics(bloomNamespace, registerer)
	if err != nil {
		return nil, err
	}
	tracker := &ipTracker{
		log: log,
		numTrackedIPs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tracked_ips",
			Help:      "Number of IPs this node is willing to dial",
		}),
		numGossipableIPs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "gossipable_ips",
			Help:      "Number of IPs this node is willing to gossip",
		}),
		bloomMetrics:         bloomMetrics,
		mostRecentTrackedIPs: make(map[ids.NodeID]*ips.ClaimedIPPort),
		bloomAdditions:       make(map[ids.NodeID]int),
		connected:            make(map[ids.NodeID]*ips.ClaimedIPPort),
		gossipableIndices:    make(map[ids.NodeID]int),
	}
	err = utils.Err(
		registerer.Register(tracker.numTrackedIPs),
		registerer.Register(tracker.numGossipableIPs),
	)
	if err != nil {
		return nil, err
	}
	return tracker, tracker.resetBloom()
}

type ipTracker struct {
	log              logging.Logger
	numTrackedIPs    prometheus.Gauge
	numGossipableIPs prometheus.Gauge
	bloomMetrics     *bloom.Metrics

	lock sync.RWMutex
	// manuallyTracked contains the nodeIDs of all nodes whose connection was
	// manually requested.
	manuallyTracked set.Set[ids.NodeID]
	// manuallyGossipable contains the nodeIDs of all nodes whose IP was
	// manually configured to be gossiped.
	manuallyGossipable set.Set[ids.NodeID]

	// mostRecentTrackedIPs tracks the most recent IP of each node whose
	// connection is desired.
	//
	// An IP is tracked if one of the following conditions are met:
	// - The node was manually tracked
	// - The node was manually requested to be gossiped
	// - The node is a validator
	mostRecentTrackedIPs map[ids.NodeID]*ips.ClaimedIPPort
	// trackedIDs contains the nodeIDs of all nodes whose connection is desired.
	trackedIDs set.Set[ids.NodeID]

	// The bloom filter contains the most recent tracked IPs to avoid
	// unnecessary IP gossip.
	bloom *bloom.Filter
	// To prevent validators from causing the bloom filter to have too many
	// false positives, we limit each validator to maxIPEntriesPerValidator in
	// the bloom filter.
	bloomAdditions map[ids.NodeID]int // Number of IPs added to the bloom
	bloomSalt      []byte
	maxBloomCount  int

	// Connected tracks the IP of currently connected peers, including tracked
	// and untracked nodes. The IP is not necessarily the same IP as in
	// mostRecentTrackedIPs.
	connected map[ids.NodeID]*ips.ClaimedIPPort

	// An IP is marked as gossipable if all of the following conditions are met:
	// - The node is a validator or was manually requested to be gossiped
	// - The node is connected
	// - The IP the node connected with is its latest IP
	gossipableIndices map[ids.NodeID]int
	// gossipableIPs is guaranteed to be a subset of [mostRecentTrackedIPs].
	gossipableIPs []*ips.ClaimedIPPort
	gossipableIDs set.Set[ids.NodeID]
}

// ManuallyTrack marks the provided nodeID as being desirable to connect to.
//
// In order for a node to learn about these nodeIDs, other nodes in the network
// must have marked them as gossipable.
//
// Even if nodes disagree on the set of manually tracked nodeIDs, they will not
// introduce persistent network gossip.
func (i *ipTracker) ManuallyTrack(nodeID ids.NodeID) {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.addTrackableID(nodeID)
	i.manuallyTracked.Add(nodeID)
}

// ManuallyGossip marks the provided nodeID as being desirable to connect to and
// marks the IPs that this node provides as being valid to gossip.
//
// In order to avoid persistent network gossip, it's important for nodes in the
// network to agree upon manually gossiped nodeIDs.
func (i *ipTracker) ManuallyGossip(nodeID ids.NodeID) {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.addTrackableID(nodeID)
	i.manuallyTracked.Add(nodeID)

	i.addGossipableID(nodeID)
	i.manuallyGossipable.Add(nodeID)
}

// WantsConnection returns true if any of the following conditions are met:
//  1. The node has been manually tracked.
//  2. The node has been manually gossiped.
//  3. The node is currently a validator.
func (i *ipTracker) WantsConnection(nodeID ids.NodeID) bool {
	i.lock.RLock()
	defer i.lock.RUnlock()

	return i.trackedIDs.Contains(nodeID)
}

// ShouldVerifyIP is used as an optimization to avoid unnecessary IP
// verification. It returns true if all of the following conditions are met:
//  1. The provided IP is from a node whose connection is desired.
//  2. This IP is newer than the most recent IP we know of for the node.
func (i *ipTracker) ShouldVerifyIP(ip *ips.ClaimedIPPort) bool {
	i.lock.RLock()
	defer i.lock.RUnlock()

	if !i.trackedIDs.Contains(ip.NodeID) {
		return false
	}

	prevIP, ok := i.mostRecentTrackedIPs[ip.NodeID]
	return !ok || // This would be the first IP
		prevIP.Timestamp < ip.Timestamp // This would be a newer IP
}

// AddIP attempts to update the node's IP to the provided IP. This function
// assumes the provided IP has been verified. Returns true if all of the
// following conditions are met:
//  1. The provided IP is from a node whose connection is desired.
//  2. This IP is newer than the most recent IP we know of for the node.
//
// If the previous IP was marked as gossipable, calling this function will
// remove the IP from the gossipable set.
func (i *ipTracker) AddIP(ip *ips.ClaimedIPPort) bool {
	i.lock.Lock()
	defer i.lock.Unlock()

	return i.addIP(ip) > sameTimestamp
}

// GetIP returns the most recent IP of the provided nodeID. If a connection to
// this nodeID is not desired, this function will return false.
func (i *ipTracker) GetIP(nodeID ids.NodeID) (*ips.ClaimedIPPort, bool) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	ip, ok := i.mostRecentTrackedIPs[nodeID]
	return ip, ok
}

// Connected is called when a connection is established. The peer should have
// provided [ip] during the handshake.
func (i *ipTracker) Connected(ip *ips.ClaimedIPPort) {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.connected[ip.NodeID] = ip
	if i.addIP(ip) >= sameTimestamp && i.gossipableIDs.Contains(ip.NodeID) {
		i.addGossipableIP(ip)
	}
}

func (i *ipTracker) addIP(ip *ips.ClaimedIPPort) int {
	if !i.trackedIDs.Contains(ip.NodeID) {
		return untrackedTimestamp
	}

	prevIP, ok := i.mostRecentTrackedIPs[ip.NodeID]
	if !ok {
		// This is the first IP we've heard from the validator, so it is the
		// most recent.
		i.updateMostRecentTrackedIP(ip)
		// Because we didn't previously have an IP, we know we aren't currently
		// connected to them.
		return newTimestamp
	}

	if prevIP.Timestamp > ip.Timestamp {
		return olderTimestamp // This IP is old than the previously known IP.
	}
	if prevIP.Timestamp == ip.Timestamp {
		return sameTimestamp // This IP is equal to the previously known IP.
	}

	i.updateMostRecentTrackedIP(ip)
	i.removeGossipableIP(ip.NodeID)
	return newerTimestamp
}

// Disconnected is called when a connection to the peer is closed.
func (i *ipTracker) Disconnected(nodeID ids.NodeID) {
	i.lock.Lock()
	defer i.lock.Unlock()

	delete(i.connected, nodeID)
	i.removeGossipableIP(nodeID)
}

func (i *ipTracker) OnValidatorAdded(nodeID ids.NodeID, _ *bls.PublicKey, _ ids.ID, _ uint64) {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.addTrackableID(nodeID)
	i.addGossipableID(nodeID)
}

func (i *ipTracker) addTrackableID(nodeID ids.NodeID) {
	if i.trackedIDs.Contains(nodeID) {
		return
	}

	i.trackedIDs.Add(nodeID)
	ip, connected := i.connected[nodeID]
	if !connected {
		return
	}

	// Because we previously weren't tracking this nodeID, the IP from the
	// connection is guaranteed to be the most up-to-date IP that we know.
	i.updateMostRecentTrackedIP(ip)
}

func (i *ipTracker) addGossipableID(nodeID ids.NodeID) {
	if i.gossipableIDs.Contains(nodeID) {
		return
	}

	i.gossipableIDs.Add(nodeID)
	connectedIP, connected := i.connected[nodeID]
	if !connected {
		return
	}

	if updatedIP, ok := i.mostRecentTrackedIPs[nodeID]; !ok || connectedIP.Timestamp != updatedIP.Timestamp {
		return
	}

	i.addGossipableIP(connectedIP)
}

func (*ipTracker) OnValidatorWeightChanged(ids.NodeID, uint64, uint64) {}

func (i *ipTracker) OnValidatorRemoved(nodeID ids.NodeID, _ uint64) {
	i.lock.Lock()
	defer i.lock.Unlock()

	if i.manuallyGossipable.Contains(nodeID) {
		return
	}

	i.gossipableIDs.Remove(nodeID)
	i.removeGossipableIP(nodeID)

	if i.manuallyTracked.Contains(nodeID) {
		return
	}

	i.trackedIDs.Remove(nodeID)
	delete(i.mostRecentTrackedIPs, nodeID)
	i.numTrackedIPs.Set(float64(len(i.mostRecentTrackedIPs)))
}

func (i *ipTracker) updateMostRecentTrackedIP(ip *ips.ClaimedIPPort) {
	i.mostRecentTrackedIPs[ip.NodeID] = ip
	i.numTrackedIPs.Set(float64(len(i.mostRecentTrackedIPs)))

	oldCount := i.bloomAdditions[ip.NodeID]
	if oldCount >= maxIPEntriesPerNode {
		return
	}

	// If the validator set is growing rapidly, we should increase the size of
	// the bloom filter.
	if count := i.bloom.Count(); count >= i.maxBloomCount {
		if err := i.resetBloom(); err != nil {
			i.log.Error("failed to reset validator tracker bloom filter",
				zap.Int("maxCount", i.maxBloomCount),
				zap.Int("currentCount", count),
				zap.Error(err),
			)
		} else {
			i.log.Info("reset validator tracker bloom filter",
				zap.Int("currentCount", count),
			)
		}
		return
	}

	i.bloomAdditions[ip.NodeID] = oldCount + 1
	bloom.Add(i.bloom, ip.GossipID[:], i.bloomSalt)
	i.bloomMetrics.Count.Inc()
}

func (i *ipTracker) addGossipableIP(ip *ips.ClaimedIPPort) {
	i.gossipableIndices[ip.NodeID] = len(i.gossipableIPs)
	i.gossipableIPs = append(i.gossipableIPs, ip)
	i.numGossipableIPs.Inc()
}

func (i *ipTracker) removeGossipableIP(nodeID ids.NodeID) {
	indexToRemove, wasGossipable := i.gossipableIndices[nodeID]
	if !wasGossipable {
		return
	}

	newNumGossipable := len(i.gossipableIPs) - 1
	if newNumGossipable != indexToRemove {
		replacementIP := i.gossipableIPs[newNumGossipable]
		i.gossipableIndices[replacementIP.NodeID] = indexToRemove
		i.gossipableIPs[indexToRemove] = replacementIP
	}

	delete(i.gossipableIndices, nodeID)
	i.gossipableIPs[newNumGossipable] = nil
	i.gossipableIPs = i.gossipableIPs[:newNumGossipable]
	i.numGossipableIPs.Dec()
}

// GetGossipableIPs returns the latest IPs of connected validators. The returned
// IPs will not contain [exceptNodeID] or any IPs contained in [exceptIPs]. If
// the number of eligible IPs to return low, it's possible that every IP will be
// iterated over while handling this call.
func (i *ipTracker) GetGossipableIPs(
	exceptNodeID ids.NodeID,
	exceptIPs *bloom.ReadFilter,
	salt []byte,
	maxNumIPs int,
) []*ips.ClaimedIPPort {
	var (
		uniform = sampler.NewUniform()
		ips     = make([]*ips.ClaimedIPPort, 0, maxNumIPs)
	)

	i.lock.RLock()
	defer i.lock.RUnlock()

	uniform.Initialize(uint64(len(i.gossipableIPs)))
	for len(ips) < maxNumIPs {
		index, hasNext := uniform.Next()
		if !hasNext {
			return ips
		}

		ip := i.gossipableIPs[index]
		if ip.NodeID == exceptNodeID {
			continue
		}

		if !bloom.Contains(exceptIPs, ip.GossipID[:], salt) {
			ips = append(ips, ip)
		}
	}
	return ips
}

// ResetBloom prunes the current bloom filter. This must be called periodically
// to ensure that validators that change their IPs are updated correctly and
// that validators that left the validator set are removed.
func (i *ipTracker) ResetBloom() error {
	i.lock.Lock()
	defer i.lock.Unlock()

	return i.resetBloom()
}

// Bloom returns the binary representation of the bloom filter along with the
// random salt.
func (i *ipTracker) Bloom() ([]byte, []byte) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	return i.bloom.Marshal(), i.bloomSalt
}

// resetBloom creates a new bloom filter with a reasonable size for the current
// validator set size. This function additionally populates the new bloom filter
// with the current most recently known IPs of validators.
func (i *ipTracker) resetBloom() error {
	newSalt := make([]byte, saltSize)
	_, err := rand.Reader.Read(newSalt)
	if err != nil {
		return err
	}

	count := max(maxIPEntriesPerNode*i.trackedIDs.Len(), minCountEstimate)
	numHashes, numEntries := bloom.OptimalParameters(
		count,
		targetFalsePositiveProbability,
	)
	newFilter, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return err
	}

	i.bloom = newFilter
	clear(i.bloomAdditions)
	i.bloomSalt = newSalt
	i.maxBloomCount = bloom.EstimateCount(numHashes, numEntries, maxFalsePositiveProbability)

	for nodeID, ip := range i.mostRecentTrackedIPs {
		bloom.Add(newFilter, ip.GossipID[:], newSalt)
		i.bloomAdditions[nodeID] = 1
	}
	i.bloomMetrics.Reset(newFilter, i.maxBloomCount)
	return nil
}

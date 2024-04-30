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
	"github.com/ava-labs/avalanchego/utils/constants"
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

var _ validators.ManagerCallbackListener = (*ipTracker)(nil)

func newIPTracker(
	trackedSubnets set.Set[ids.ID],
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
		trackedSubnets: trackedSubnets,
		log:            log,
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
		bloomMetrics:   bloomMetrics,
		tracked:        make(map[ids.NodeID]*trackedNode),
		bloomAdditions: make(map[ids.NodeID]int),
		connected:      make(map[ids.NodeID]*connectedNode),
		subnet:         make(map[ids.ID]*gossipableSubnet),

		mostRecentTrackedIPs: make(map[ids.NodeID]*ips.ClaimedIPPort),
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

// A node is tracked if any of the following conditions are met:
// - The node was manually tracked
// - The node is a validator on any subnet
type trackedNode struct {
	// manuallyTracked tracks if this node's connection was manually requested.
	manuallyTracked bool
	// subnets contains all the subnets that this node is a validator of.
	subnets set.Set[ids.ID]
	// subnets contains the subset of [subnets] that the local node also tracks.
	trackedSubnets set.Set[ids.ID]
	// ip is the most recently known IP of this node.
	ip *ips.ClaimedIPPort
}

func (n *trackedNode) wantsConnection() bool {
	return n.manuallyTracked || n.trackedSubnets.Len() > 0
}

type connectedNode struct {
	// trackedSubnets contains all the subnets that this node is syncing.
	trackedSubnets set.Set[ids.ID]
	// ip this node claimed when connecting to use. The IP is not necessarily
	// the same IP as in mostRecentTrackedIPs.
	ip *ips.ClaimedIPPort
}

type gossipableSubnet struct {
	// manuallyGossipable contains the nodeIDs of all nodes whose IP was
	// manually configured to be gossiped for this subnet.
	manuallyGossipable set.Set[ids.NodeID]

	// An IP is marked as gossipable if all of the following conditions are met:
	// - The node is a validator or was manually requested to be gossiped
	// - The node is connected
	// - The node reported that they are syncing this subnet
	// - The IP the node connected with is its latest IP
	gossipableIndices map[ids.NodeID]int
	gossipableIPs     []*ips.ClaimedIPPort
	gossipableIDs     set.Set[ids.NodeID]
}

type ipTracker struct {
	trackedSubnets   set.Set[ids.ID]
	log              logging.Logger
	numTrackedIPs    prometheus.Gauge
	numGossipableIPs prometheus.Gauge
	bloomMetrics     *bloom.Metrics

	lock    sync.RWMutex
	tracked map[ids.NodeID]*trackedNode

	// The bloom filter contains the most recent tracked IPs to avoid
	// unnecessary IP gossip.
	bloom *bloom.Filter
	// To prevent validators from causing the bloom filter to have too many
	// false positives, we limit each validator to maxIPEntriesPerValidator in
	// the bloom filter.
	bloomAdditions map[ids.NodeID]int // Number of IPs added to the bloom
	bloomSalt      []byte
	maxBloomCount  int

	// Connected tracks the information of currently connected peers, including
	// tracked and untracked nodes.
	connected map[ids.NodeID]*connectedNode
	subnet    map[ids.ID]*gossipableSubnet

	// TODO: Remove everything below this line

	// manuallyTracked contains the nodeIDs of all nodes whose connection was
	// manually requested.
	manuallyTracked set.Set[ids.NodeID]

	// TODO: Should include what subnets are causing the IPs to be tracked
	// mostRecentTrackedIPs tracks the most recent IP of each node whose
	// connection is desired.
	//
	// An IP is tracked if any of the following conditions are met:
	// - The node was manually tracked
	// - The node was manually requested to be gossiped
	// - The node is a validator
	mostRecentTrackedIPs map[ids.NodeID]*ips.ClaimedIPPort
	// trackedIDs contains the nodeIDs of all nodes whose connection is desired.
	trackedIDs set.Set[ids.NodeID]

	// TODO: Should be by subnet
	// manuallyGossipable contains the nodeIDs of all nodes whose IP was
	// manually configured to be gossiped.
	manuallyGossipable set.Set[ids.NodeID]

	// TODO: Should be by subnet
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

	i.addTrackableID(nodeID, nil)
}

// ManuallyGossip marks the provided nodeID as being desirable to connect to and
// marks the IPs that this node provides as being valid to gossip.
//
// In order to avoid persistent network gossip, it's important for nodes in the
// network to agree upon manually gossiped nodeIDs.
func (i *ipTracker) ManuallyGossip(subnetID ids.ID, nodeID ids.NodeID) {
	if subnetID != constants.PrimaryNetworkID {
		return
	}

	i.lock.Lock()
	defer i.lock.Unlock()

	if subnetID == constants.PrimaryNetworkID || i.trackedSubnets.Contains(subnetID) {
		i.addTrackableID(nodeID, nil)
	}

	i.addGossipableID(nodeID)
	i.manuallyGossipable.Add(nodeID)
}

// WantsConnection returns true if any of the following conditions are met:
//  1. The node has been manually tracked.
//  2. The node has been manually gossiped on a tracked subnet.
//  3. The node is currently a validator on a tracked subnet.
func (i *ipTracker) WantsConnection(nodeID ids.NodeID) bool {
	i.lock.RLock()
	defer i.lock.RUnlock()

	node, ok := i.tracked[nodeID]
	return ok && node.wantsConnection()
}

// ShouldVerifyIP is used as an optimization to avoid unnecessary IP
// verification. It returns true if all of the following conditions are met:
//  1. The provided IP is from a node whose connection is desired on any subnet.
//  2. This IP is newer than the most recent IP we know of for the node.
func (i *ipTracker) ShouldVerifyIP(ip *ips.ClaimedIPPort) bool {
	i.lock.RLock()
	defer i.lock.RUnlock()

	node, ok := i.tracked[ip.NodeID]
	if !ok {
		return false
	}

	return node.ip == nil || // This would be the first IP
		node.ip.Timestamp < ip.Timestamp // This would be a newer IP
}

// AddIP attempts to update the node's IP to the provided IP. This function
// assumes the provided IP has been verified. Returns true if all of the
// following conditions are met:
//  1. The provided IP is from a node whose connection is desired on a tracked
//     subnet.
//  2. This IP is newer than the most recent IP we know of for the node.
//
// If the previous IP for this node was marked as gossipable, calling this
// function will remove the previous IP from the gossipable set.
func (i *ipTracker) AddIP(ip *ips.ClaimedIPPort) bool {
	i.lock.Lock()
	defer i.lock.Unlock()

	timestampComparison, wantsConnection := i.addIP(ip)
	return wantsConnection && timestampComparison > sameTimestamp
}

// GetIP returns the most recent IP of the provided nodeID. Returns true if all
// of the following conditions are met:
//  1. There is currently an IP for the provided nodeID.
//  1. The provided IP is from a node whose connection is desired on a tracked
//     subnet.
func (i *ipTracker) GetIP(nodeID ids.NodeID) (*ips.ClaimedIPPort, bool) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	node, ok := i.tracked[nodeID]
	if !ok || node.ip == nil {
		return nil, false
	}
	return node.ip, node.wantsConnection()
}

// Connected is called when a connection is established. The peer should have
// provided [ip] during the handshake.
func (i *ipTracker) Connected(ip *ips.ClaimedIPPort, trackedSubnets set.Set[ids.ID]) {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.connected[ip.NodeID] = &connectedNode{
		trackedSubnets: trackedSubnets,
		ip:             ip,
	}
	timestampComparison, _ := i.addIP(ip)
	if timestampComparison >= sameTimestamp && i.gossipableIDs.Contains(ip.NodeID) {
		i.addGossipableIP(ip)
	}
}

func (i *ipTracker) addIP(ip *ips.ClaimedIPPort) (int, bool) {
	node, ok := i.tracked[ip.NodeID]
	if !ok {
		return untrackedTimestamp, false
	}

	if node.ip == nil {
		// This is the first IP we've heard from the validator, so it is the
		// most recent.
		i.updateMostRecentTrackedIP(ip)
		// Because we didn't previously have an IP, we know we aren't currently
		// connected to them.
		return newTimestamp, node.wantsConnection()
	}

	if node.ip.Timestamp > ip.Timestamp {
		return olderTimestamp, false // This IP is older than the previously known IP.
	}
	if node.ip.Timestamp == ip.Timestamp {
		return sameTimestamp, false // This IP is equal to the previously known IP.
	}

	i.updateMostRecentTrackedIP(ip)
	i.removeGossipableIP(ip.NodeID)
	return newerTimestamp, node.wantsConnection()
}

// Disconnected is called when a connection to the peer is closed.
func (i *ipTracker) Disconnected(nodeID ids.NodeID) {
	i.lock.Lock()
	defer i.lock.Unlock()

	delete(i.connected, nodeID)
	i.removeGossipableIP(nodeID)
}

func (i *ipTracker) OnValidatorAdded(subnetID ids.ID, nodeID ids.NodeID, _ *bls.PublicKey, _ ids.ID, _ uint64) {
	if subnetID != constants.PrimaryNetworkID {
		return
	}

	i.lock.Lock()
	defer i.lock.Unlock()

	i.addTrackableID(nodeID, &subnetID)
	i.addGossipableID(nodeID)
}

func (i *ipTracker) addTrackableID(nodeID ids.NodeID, subnetID *ids.ID) {
	nodeTracker, previouslyTracked := i.tracked[nodeID]
	if !previouslyTracked {
		nodeTracker = &trackedNode{}
	}

	nodeTracker.manuallyTracked = nodeTracker.manuallyTracked || subnetID == nil
	if subnetID == nil {
		nodeTracker.manuallyTracked = true
		i.manuallyTracked.Add(nodeID)
	} else {
		nodeTracker.subnets.Add(*subnetID)
	}

	if previouslyTracked {
		return
	}

	i.trackedIDs.Add(nodeID)
	node, connected := i.connected[nodeID]
	if !connected {
		return
	}

	// Because we previously weren't tracking this nodeID, the IP from the
	// connection is guaranteed to be the most up-to-date IP that we know.
	i.updateMostRecentTrackedIP(node.ip)
}

func (i *ipTracker) addGossipableID(nodeID ids.NodeID) {
	if i.gossipableIDs.Contains(nodeID) {
		return
	}

	i.gossipableIDs.Add(nodeID)
	node, connected := i.connected[nodeID]
	if !connected {
		return
	}

	if updatedIP, ok := i.mostRecentTrackedIPs[nodeID]; !ok || node.ip.Timestamp != updatedIP.Timestamp {
		return
	}

	i.addGossipableIP(node.ip)
}

func (*ipTracker) OnValidatorWeightChanged(ids.ID, ids.NodeID, uint64, uint64) {}

func (i *ipTracker) OnValidatorRemoved(subnetID ids.ID, nodeID ids.NodeID, _ uint64) {
	if subnetID != constants.PrimaryNetworkID {
		return
	}

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
	i.tracked[ip.NodeID].ip = ip
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

// GetGossipableIPs returns the latest IPs of connected validators. If
// [trackedSubnets] is non-nil, only IPs of validators in the provided subnets
// or the primary network are returned. The returned IPs will not contain
// [exceptNodeID] or any IPs contained in [exceptIPs]. If the number of eligible
// IPs to return low, it's possible that every IP will be iterated over while
// handling this call.
func (i *ipTracker) GetGossipableIPs(
	trackedSubnets *set.Set[ids.ID],
	exceptNodeID ids.NodeID,
	exceptIPs *bloom.ReadFilter,
	salt []byte,
	maxNumIPs int,
) []*ips.ClaimedIPPort {
	_ = trackedSubnets

	var (
		uniform = sampler.NewUniform()
		ips     = make([]*ips.ClaimedIPPort, 0, maxNumIPs)
	)

	i.lock.RLock()
	defer i.lock.RUnlock()

	uniform.Initialize(uint64(len(i.gossipableIPs)))
	for len(ips) < maxNumIPs {
		index, err := uniform.Next()
		if err != nil {
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

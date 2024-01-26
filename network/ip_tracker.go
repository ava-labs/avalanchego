// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto/rand"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	saltSize                       = 32
	minCountEstimate               = 128
	targetFalsePositiveProbability = .001
	maxFalsePositiveProbability    = .01
	// By setting maxIPEntriesPerValidator > 1, we allow validators to update
	// their IP at least once per bloom filter reset.
	maxIPEntriesPerValidator = 2
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
		numValidatorIPs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "validator_ips",
			Help:      "Number of known validator IPs",
		}),
		numGossipable: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "gossipable_ips",
			Help:      "Number of IPs this node is willing to gossip",
		}),
		bloomMetrics:           bloomMetrics,
		connected:              make(map[ids.NodeID]*ips.ClaimedIPPort),
		mostRecentValidatorIPs: make(map[ids.NodeID]*ips.ClaimedIPPort),
		gossipableIndicies:     make(map[ids.NodeID]int),
		bloomAdditions:         make(map[ids.NodeID]int),
	}
	err = utils.Err(
		registerer.Register(tracker.numValidatorIPs),
		registerer.Register(tracker.numGossipable),
	)
	if err != nil {
		return nil, err
	}
	return tracker, tracker.resetBloom()
}

type ipTracker struct {
	log             logging.Logger
	numValidatorIPs prometheus.Gauge
	numGossipable   prometheus.Gauge
	bloomMetrics    *bloom.Metrics

	lock sync.RWMutex
	// Manually tracked nodes are always treated like validators
	manuallyTracked set.Set[ids.NodeID]
	// Connected tracks the currently connected peers, including validators and
	// non-validators. The IP is not necessarily the same IP as in
	// mostRecentIPs.
	connected              map[ids.NodeID]*ips.ClaimedIPPort
	mostRecentValidatorIPs map[ids.NodeID]*ips.ClaimedIPPort
	validators             set.Set[ids.NodeID]

	// An IP is marked as gossipable if:
	// - The node is a validator
	// - The node is connected
	// - The IP the node connected with is its latest IP
	gossipableIndicies map[ids.NodeID]int
	gossipableIPs      []*ips.ClaimedIPPort

	// The bloom filter contains the most recent validator IPs to avoid
	// unnecessary IP gossip.
	bloom *bloom.Filter
	// To prevent validators from causing the bloom filter to have too many
	// false positives, we limit each validator to maxIPEntriesPerValidator in
	// the bloom filter.
	bloomAdditions map[ids.NodeID]int // Number of IPs added to the bloom
	bloomSalt      []byte
	maxBloomCount  int
}

func (i *ipTracker) ManuallyTrack(nodeID ids.NodeID) {
	i.lock.Lock()
	defer i.lock.Unlock()

	// We treat manually tracked nodes as if they were validators.
	if !i.validators.Contains(nodeID) {
		i.onValidatorAdded(nodeID)
	}
	// Now that the node is marked as a validator, freeze it's validation
	// status. Future calls to OnValidatorAdded or OnValidatorRemoved will be
	// treated as noops.
	i.manuallyTracked.Add(nodeID)
}

func (i *ipTracker) WantsConnection(nodeID ids.NodeID) bool {
	i.lock.RLock()
	defer i.lock.RUnlock()

	return i.validators.Contains(nodeID)
}

func (i *ipTracker) ShouldVerifyIP(ip *ips.ClaimedIPPort) bool {
	i.lock.RLock()
	defer i.lock.RUnlock()

	if !i.validators.Contains(ip.NodeID) {
		return false
	}

	prevIP, ok := i.mostRecentValidatorIPs[ip.NodeID]
	return !ok || // This would be the first IP
		prevIP.Timestamp < ip.Timestamp // This would be a newer IP
}

// AddIP returns true if the addition of the provided IP updated the most
// recently known IP of a validator.
func (i *ipTracker) AddIP(ip *ips.ClaimedIPPort) bool {
	i.lock.Lock()
	defer i.lock.Unlock()

	if !i.validators.Contains(ip.NodeID) {
		return false
	}

	prevIP, ok := i.mostRecentValidatorIPs[ip.NodeID]
	if !ok {
		// This is the first IP we've heard from the validator, so it is the
		// most recent.
		i.updateMostRecentValidatorIP(ip)
		// Because we didn't previously have an IP, we know we aren't currently
		// connected to them.
		return true
	}

	if prevIP.Timestamp >= ip.Timestamp {
		// This IP is not newer than the previously known IP.
		return false
	}

	i.updateMostRecentValidatorIP(ip)
	i.removeGossipableIP(ip.NodeID)
	return true
}

func (i *ipTracker) GetIP(nodeID ids.NodeID) (*ips.ClaimedIPPort, bool) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	ip, ok := i.mostRecentValidatorIPs[nodeID]
	return ip, ok
}

func (i *ipTracker) Connected(ip *ips.ClaimedIPPort) {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.connected[ip.NodeID] = ip
	if !i.validators.Contains(ip.NodeID) {
		return
	}

	prevIP, ok := i.mostRecentValidatorIPs[ip.NodeID]
	if !ok {
		// This is the first IP we've heard from the validator, so it is the
		// most recent.
		i.updateMostRecentValidatorIP(ip)
		i.addGossipableIP(ip)
		return
	}

	if prevIP.Timestamp > ip.Timestamp {
		// There is a more up-to-date IP than the one that was used to connect.
		return
	}

	if prevIP.Timestamp < ip.Timestamp {
		i.updateMostRecentValidatorIP(ip)
	}
	i.addGossipableIP(ip)
}

func (i *ipTracker) Disconnected(nodeID ids.NodeID) {
	i.lock.Lock()
	defer i.lock.Unlock()

	delete(i.connected, nodeID)
	i.removeGossipableIP(nodeID)
}

func (i *ipTracker) OnValidatorAdded(nodeID ids.NodeID, _ *bls.PublicKey, _ ids.ID, _ uint64) {
	i.lock.Lock()
	defer i.lock.Unlock()

	i.onValidatorAdded(nodeID)
}

func (i *ipTracker) onValidatorAdded(nodeID ids.NodeID) {
	if i.manuallyTracked.Contains(nodeID) {
		return
	}

	i.validators.Add(nodeID)
	ip, connected := i.connected[nodeID]
	if !connected {
		return
	}

	// Because we only track validator IPs, the from the connection is
	// guaranteed to be the most up-to-date IP that we know.
	i.updateMostRecentValidatorIP(ip)
	i.addGossipableIP(ip)
}

func (*ipTracker) OnValidatorWeightChanged(ids.NodeID, uint64, uint64) {}

func (i *ipTracker) OnValidatorRemoved(nodeID ids.NodeID, _ uint64) {
	i.lock.Lock()
	defer i.lock.Unlock()

	if i.manuallyTracked.Contains(nodeID) {
		return
	}

	delete(i.mostRecentValidatorIPs, nodeID)
	i.numValidatorIPs.Set(float64(len(i.mostRecentValidatorIPs)))

	i.validators.Remove(nodeID)
	i.removeGossipableIP(nodeID)
}

func (i *ipTracker) updateMostRecentValidatorIP(ip *ips.ClaimedIPPort) {
	i.mostRecentValidatorIPs[ip.NodeID] = ip
	i.numValidatorIPs.Set(float64(len(i.mostRecentValidatorIPs)))

	oldCount := i.bloomAdditions[ip.NodeID]
	if oldCount >= maxIPEntriesPerValidator {
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
	i.gossipableIndicies[ip.NodeID] = len(i.gossipableIPs)
	i.gossipableIPs = append(i.gossipableIPs, ip)
	i.numGossipable.Inc()
}

func (i *ipTracker) removeGossipableIP(nodeID ids.NodeID) {
	indexToRemove, wasGossipable := i.gossipableIndicies[nodeID]
	if !wasGossipable {
		return
	}

	newNumGossipable := len(i.gossipableIPs) - 1
	if newNumGossipable != indexToRemove {
		replacementIP := i.gossipableIPs[newNumGossipable]
		i.gossipableIndicies[replacementIP.NodeID] = indexToRemove
		i.gossipableIPs[indexToRemove] = replacementIP
	}

	delete(i.gossipableIndicies, nodeID)
	i.gossipableIPs[newNumGossipable] = nil
	i.gossipableIPs = i.gossipableIPs[:newNumGossipable]
	i.numGossipable.Dec()
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

	count := math.Max(maxIPEntriesPerValidator*i.validators.Len(), minCountEstimate)
	numHashes, numEntries := bloom.OptimalParameters(
		count,
		targetFalsePositiveProbability,
	)
	newFilter, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return err
	}

	i.bloom = newFilter
	maps.Clear(i.bloomAdditions)
	i.bloomSalt = newSalt
	i.maxBloomCount = bloom.EstimateCount(numHashes, numEntries, maxFalsePositiveProbability)

	for nodeID, ip := range i.mostRecentValidatorIPs {
		bloom.Add(newFilter, ip.GossipID[:], newSalt)
		i.bloomAdditions[nodeID] = 1
	}
	i.bloomMetrics.Reset(newFilter, i.maxBloomCount)
	return nil
}

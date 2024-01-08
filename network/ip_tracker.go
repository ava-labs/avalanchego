// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto/rand"
	"sync"

	"go.uber.org/zap"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	minCountEstimate               = 128
	targetFalsePositiveProbability = .001
	maxFalsePositiveProbability    = .01
	// By setting maxIPEntriesPerValidator > 1, we allow validators to update
	// their IP at least once per bloom filter recent.
	maxIPEntriesPerValidator = 2
)

var _ validators.SetCallbackListener = (*ipTracker)(nil)

func newIPTracker(log logging.Logger) (*ipTracker, error) {
	tracker := &ipTracker{
		log:                    log,
		connected:              make(map[ids.NodeID]*ips.ClaimedIPPort),
		mostRecentValidatorIPs: make(map[ids.NodeID]*ips.ClaimedIPPort),
		gossipableIndicies:     make(map[ids.NodeID]int),
		bloomAdditions:         make(map[ids.NodeID]int),
	}
	return tracker, tracker.resetBloom()
}

type ipTracker struct {
	log logging.Logger

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
	bloomSalt      ids.ID
	maxBloomCount  int
}

func (v *ipTracker) ManuallyTrack(nodeID ids.NodeID) {
	v.lock.Lock()
	defer v.lock.Unlock()

	if !v.validators.Contains(nodeID) {
		v.onValidatorAdded(nodeID)
	}
	v.manuallyTracked.Add(nodeID)
}

func (v *ipTracker) WantsConnection(nodeID ids.NodeID) bool {
	v.lock.RLock()
	defer v.lock.RUnlock()

	return v.validators.Contains(nodeID) || v.manuallyTracked.Contains(nodeID)
}

func (v *ipTracker) ShouldVerifyIP(ip *ips.ClaimedIPPort) bool {
	nodeID := ip.NodeID()

	v.lock.RLock()
	defer v.lock.RUnlock()

	if !v.validators.Contains(nodeID) {
		return false
	}

	prevIP, ok := v.mostRecentValidatorIPs[nodeID]
	return !ok || // This would be the first IP
		prevIP.Timestamp < ip.Timestamp // This would be a newer IP
}

// AddIP returns true if the addition of the provided IP updated the most
// recently known IP of a validator.
func (v *ipTracker) AddIP(ip *ips.ClaimedIPPort) bool {
	nodeID := ip.NodeID()

	v.lock.Lock()
	defer v.lock.Unlock()

	if !v.validators.Contains(nodeID) {
		return false
	}

	prevIP, ok := v.mostRecentValidatorIPs[nodeID]
	if !ok {
		// This is the first IP we've heard from the validator, so it is the
		// most recent.
		v.updateMostRecentValidatorIP(nodeID, ip)
		// Because we didn't previously have an IP, we know we aren't currently
		// connected to them.
		return true
	}

	if prevIP.Timestamp >= ip.Timestamp {
		// This IP is not newer than the previously known IP.
		return false
	}

	v.updateMostRecentValidatorIP(nodeID, ip)
	v.removeGossipableIP(nodeID)
	return true
}

func (v *ipTracker) GetIP(nodeID ids.NodeID) (*ips.ClaimedIPPort, bool) {
	v.lock.RLock()
	defer v.lock.RUnlock()

	ip, ok := v.mostRecentValidatorIPs[nodeID]
	return ip, ok
}

func (v *ipTracker) Connected(ip *ips.ClaimedIPPort) {
	nodeID := ip.NodeID()

	v.lock.Lock()
	defer v.lock.Unlock()

	v.connected[nodeID] = ip
	if !v.validators.Contains(nodeID) {
		return
	}

	prevIP, ok := v.mostRecentValidatorIPs[nodeID]
	if !ok {
		// This is the first IP we've heard from the validator, so it is the
		// most recent.
		v.updateMostRecentValidatorIP(nodeID, ip)
		v.addGossipableIP(nodeID, ip)
		return
	}

	if prevIP.Timestamp > ip.Timestamp {
		// There is a more up-to-date IP than the one that was used to connect.
		return
	}

	if prevIP.Timestamp < ip.Timestamp {
		v.updateMostRecentValidatorIP(nodeID, ip)
	}
	v.addGossipableIP(nodeID, ip)
}

func (v *ipTracker) Disconnected(nodeID ids.NodeID) {
	v.lock.Lock()
	defer v.lock.Unlock()

	delete(v.connected, nodeID)
	v.removeGossipableIP(nodeID)
}

func (v *ipTracker) OnValidatorAdded(nodeID ids.NodeID, _ *bls.PublicKey, _ ids.ID, _ uint64) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.onValidatorAdded(nodeID)
}

func (v *ipTracker) onValidatorAdded(nodeID ids.NodeID) {
	if v.manuallyTracked.Contains(nodeID) {
		return
	}

	v.validators.Add(nodeID)
	ip, connected := v.connected[nodeID]
	if !connected {
		return
	}

	v.updateMostRecentValidatorIP(nodeID, ip)
	v.addGossipableIP(nodeID, ip)
}

func (*ipTracker) OnValidatorWeightChanged(ids.NodeID, uint64, uint64) {}

func (v *ipTracker) OnValidatorRemoved(nodeID ids.NodeID, _ uint64) {
	v.lock.Lock()
	defer v.lock.Unlock()

	if v.manuallyTracked.Contains(nodeID) {
		return
	}

	delete(v.mostRecentValidatorIPs, nodeID)
	v.validators.Remove(nodeID)
	v.removeGossipableIP(nodeID)
}

func (v *ipTracker) updateMostRecentValidatorIP(nodeID ids.NodeID, ip *ips.ClaimedIPPort) {
	v.mostRecentValidatorIPs[nodeID] = ip
	oldCount := v.bloomAdditions[nodeID]
	if oldCount >= maxIPEntriesPerValidator {
		return
	}

	// If the validator set is growing rapidly, we should increase the size of
	// the bloom filter.
	if count := v.bloom.Count(); count >= v.maxBloomCount {
		if err := v.resetBloom(); err != nil {
			v.log.Error("failed to reset validator tracker bloom filter",
				zap.Int("maxCount", v.maxBloomCount),
				zap.Int("currentCount", count),
				zap.Error(err),
			)
		} else {
			v.log.Info("reset validator tracker bloom filter",
				zap.Int("currentCount", count),
			)
		}
		return
	}

	v.bloomAdditions[nodeID] = oldCount + 1
	gossipID := ip.GossipID()
	bloom.Add(v.bloom, gossipID[:], v.bloomSalt[:])
}

func (v *ipTracker) addGossipableIP(nodeID ids.NodeID, ip *ips.ClaimedIPPort) {
	v.gossipableIndicies[nodeID] = len(v.gossipableIPs)
	v.gossipableIPs = append(v.gossipableIPs, ip)
}

func (v *ipTracker) removeGossipableIP(nodeID ids.NodeID) {
	indexToRemove, wasGossipable := v.gossipableIndicies[nodeID]
	if !wasGossipable {
		return
	}

	newNumGossipable := len(v.gossipableIPs) - 1
	if newNumGossipable != indexToRemove {
		replacementIP := v.gossipableIPs[newNumGossipable]
		replacementNodeID := replacementIP.NodeID()
		v.gossipableIndicies[replacementNodeID] = indexToRemove
		v.gossipableIPs[indexToRemove] = replacementIP
	}

	delete(v.gossipableIndicies, nodeID)
	v.gossipableIPs[newNumGossipable] = nil
	v.gossipableIPs = v.gossipableIPs[:newNumGossipable]
}

func (v *ipTracker) GetValidatorIPs(exceptNodeID ids.NodeID, exceptIPs *bloom.ReadFilter, salt ids.ID, maxNumIPs int) []*ips.ClaimedIPPort {
	var (
		uniform = sampler.NewUniform()
		ips     = make([]*ips.ClaimedIPPort, 0, maxNumIPs)
	)

	v.lock.RLock()
	defer v.lock.RUnlock()

	uniform.Initialize(uint64(len(v.gossipableIPs)))
	for len(ips) < maxNumIPs {
		index, err := uniform.Next()
		if err != nil {
			return ips
		}

		ip := v.gossipableIPs[index]
		nodeID := ip.NodeID()
		if nodeID == exceptNodeID {
			continue
		}

		gossipID := ip.GossipID()
		if !bloom.Contains(exceptIPs, gossipID[:], salt[:]) {
			ips = append(ips, ip)
		}
	}
	return ips
}

// ResetBloom prunes the current bloom filter. This must be called periodically
// to ensure that validators that change their IPs are updated correctly and
// that validators that left the validator set are removed.
func (v *ipTracker) ResetBloom() error {
	v.lock.Lock()
	defer v.lock.Unlock()

	return v.resetBloom()
}

// Bloom returns the binary representation of the bloom filter along with the
// random salt.
func (v *ipTracker) Bloom() ([]byte, ids.ID) {
	v.lock.RLock()
	defer v.lock.RUnlock()

	return v.bloom.Marshal(), v.bloomSalt
}

// resetBloom creates a new bloom filter with a reasonable size for the current
// validator set size. This function additionally populates the new bloom filter
// with the current most recently known IPs of validators.
func (v *ipTracker) resetBloom() error {
	var newSalt ids.ID
	_, err := rand.Reader.Read(newSalt[:])
	if err != nil {
		return err
	}

	count := math.Max(maxIPEntriesPerValidator*v.validators.Len(), minCountEstimate)
	numHashes, numEntries := bloom.OptimalParameters(
		count,
		targetFalsePositiveProbability,
	)
	newFilter, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return err
	}

	v.bloom = newFilter
	maps.Clear(v.bloomAdditions)
	v.bloomSalt = newSalt
	v.maxBloomCount = bloom.EstimateCount(numHashes, numEntries, maxFalsePositiveProbability)

	for _, ip := range v.mostRecentValidatorIPs {
		gossipID := ip.GossipID()
		bloom.Add(newFilter, gossipID[:], newSalt[:])
		nodeID := ip.NodeID()
		v.bloomAdditions[nodeID]++
	}
	return nil
}

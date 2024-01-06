// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto/rand"
	"sync"

	"go.uber.org/zap"

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
)

var _ validators.SetCallbackListener = (*ValidatorTracker)(nil)

func NewValidatorTracker(log logging.Logger) (*ValidatorTracker, error) {
	tracker := &ValidatorTracker{
		log:                        log,
		connected:                  make(map[ids.NodeID]*ips.ClaimedIPPort),
		connectedValidatorIndicies: make(map[ids.NodeID]int),
	}
	return tracker, tracker.resetBloom()
}

type ValidatorTracker struct {
	log logging.Logger

	lock                             sync.RWMutex
	connected                        map[ids.NodeID]*ips.ClaimedIPPort
	validators                       set.Set[ids.NodeID]
	connectedValidatorIndicies       map[ids.NodeID]int
	connectedValidators              []*ips.ClaimedIPPort
	connectedValidatorsBloom         *bloom.Filter       // Bloom we use to pull new connections from other peers
	connectedValidatorsAdditions     set.Set[ids.NodeID] // Set of validatorIDs that have currently been added to the bloom filter
	connectedValidatorsBloomSalt     ids.ID
	maxConnectedValidatorsBloomCount int
}

func (v *ValidatorTracker) Connected(nodeID ids.NodeID, ip *ips.ClaimedIPPort) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.connected[nodeID] = ip
	if v.validators.Contains(nodeID) {
		v.addConnectedValidator(nodeID, ip)
	}
}

func (v *ValidatorTracker) Disconnected(nodeID ids.NodeID) *ips.ClaimedIPPort {
	v.lock.Lock()
	defer v.lock.Unlock()

	ip, ok := v.connected[nodeID]
	if !ok {
		return nil
	}

	delete(v.connected, nodeID)
	v.removeConnectedValidator(nodeID)
	return ip
}

func (v *ValidatorTracker) OnValidatorAdded(nodeID ids.NodeID, _ *bls.PublicKey, _ ids.ID, _ uint64) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.validators.Add(nodeID)
	if ip, connected := v.connected[nodeID]; connected {
		v.addConnectedValidator(nodeID, ip)
	}
}

func (*ValidatorTracker) OnValidatorWeightChanged(ids.NodeID, uint64, uint64) {}

func (v *ValidatorTracker) OnValidatorRemoved(nodeID ids.NodeID, _ uint64) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.validators.Remove(nodeID)
	v.removeConnectedValidator(nodeID)
}

func (v *ValidatorTracker) addConnectedValidator(nodeID ids.NodeID, ip *ips.ClaimedIPPort) {
	v.connectedValidatorIndicies[nodeID] = len(v.connectedValidators)
	v.connectedValidators = append(v.connectedValidators, ip)

	// If we have already added this node to the bloom filter, wait until
	// manually resetting the bloom filter to update the entry.
	if v.connectedValidatorsAdditions.Contains(nodeID) {
		return
	}

	// If the validator set is growing rapidly, we should increase the size of
	// the bloom filter.
	if count := v.connectedValidatorsBloom.Count(); count >= v.maxConnectedValidatorsBloomCount {
		if err := v.resetBloom(); err != nil {
			v.log.Error("failed to reset validator tracker bloom filter",
				zap.Int("maxCount", v.maxConnectedValidatorsBloomCount),
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

	v.connectedValidatorsAdditions.Add(nodeID)
	gossipID := ip.GossipID()
	bloom.Add(v.connectedValidatorsBloom, gossipID[:], v.connectedValidatorsBloomSalt[:])
}

func (v *ValidatorTracker) removeConnectedValidator(nodeID ids.NodeID) {
	connectedValidatorIndexToRemove, wasValidator := v.connectedValidatorIndicies[nodeID]
	if !wasValidator {
		return
	}

	newNumConnectedValidators := len(v.connectedValidators) - 1
	if newNumConnectedValidators != connectedValidatorIndexToRemove {
		replacementIP := v.connectedValidators[newNumConnectedValidators]
		replacementNodeID := replacementIP.NodeID()
		v.connectedValidatorIndicies[replacementNodeID] = connectedValidatorIndexToRemove
		v.connectedValidators[connectedValidatorIndexToRemove] = replacementIP
	}

	delete(v.connectedValidatorIndicies, nodeID)
	v.connectedValidators[newNumConnectedValidators] = nil
	v.connectedValidators = v.connectedValidators[:newNumConnectedValidators]
}

func (v *ValidatorTracker) GetValidatorIPs(exceptNodeID ids.NodeID, exceptIPs *bloom.ReadFilter, salt ids.ID, maxNumIPs int) []*ips.ClaimedIPPort {
	var (
		uniform = sampler.NewUniform()
		ips     = make([]*ips.ClaimedIPPort, 0, maxNumIPs)
	)

	v.lock.RLock()
	defer v.lock.RUnlock()

	uniform.Initialize(uint64(len(v.connectedValidators)))
	for len(ips) < maxNumIPs {
		index, err := uniform.Next()
		if err != nil {
			return ips
		}

		ip := v.connectedValidators[index]
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
func (v *ValidatorTracker) ResetBloom() error {
	v.lock.Lock()
	defer v.lock.Unlock()

	return v.resetBloom()
}

// Bloom returns the binary representation of the bloom filter along with the
// random salt.
func (v *ValidatorTracker) Bloom() ([]byte, ids.ID) {
	v.lock.RLock()
	defer v.lock.RUnlock()

	return v.connectedValidatorsBloom.Marshal(), v.connectedValidatorsBloomSalt
}

// resetBloom creates a new bloom filter with a reasonable size for the current
// validator set size. This function additionally populates the new bloom filter
// with the currently connected validators.
func (v *ValidatorTracker) resetBloom() error {
	var newSalt ids.ID
	_, err := rand.Reader.Read(newSalt[:])
	if err != nil {
		return err
	}

	count := math.Max(2*v.validators.Len(), minCountEstimate)
	numHashes, numEntries := bloom.OptimalParameters(
		count,
		targetFalsePositiveProbability,
	)
	newFilter, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return err
	}

	v.connectedValidatorsBloom = newFilter
	v.connectedValidatorsAdditions.Clear()
	v.connectedValidatorsBloomSalt = newSalt
	v.maxConnectedValidatorsBloomCount = bloom.EstimateCount(numHashes, numEntries, maxFalsePositiveProbability)

	for _, ip := range v.connectedValidators {
		gossipID := ip.GossipID()
		bloom.Add(newFilter, gossipID[:], newSalt[:])
		nodeID := ip.NodeID()
		v.connectedValidatorsAdditions.Add(nodeID)
	}
	return nil
}

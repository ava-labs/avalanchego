// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

// GossipTracker tracks the validators that we're currently aware of, as well as
// the validators we've told each peers about. This data is stored in a bitset
// to optimize space, where only N (num validators) bits will be used per peer.
//
// This is done by recording some state information of both what validators this
// node is aware of, and what validators  we've told each peer about.
// As an example, say we track three peers and three validators (MSB first):
//
//	trackedPeers:	{
//		p1: [1, 1, 1] // we have already told [p1] about all validators
//		p2: [0, 1, 1] // [p2] doesn't know about [v3]
//		p3: [0, 0, 1] // [p3] knows only about [v3]
//	}
//
// GetUnknown computes the validators we haven't sent to a given peer. Ex:
//
//	GetUnknown(p1) -  [0, 0, 0]
//	GetUnknown(p2) -  [1, 0, 0]
//	GetUnknown(p3) -  [1, 1, 0]
//
// Using the gossipTracker, we can quickly compute the validators each peer
// doesn't know about using GetUnknown so that in subsequent PeerList gossip
// messages we only send information that this peer (most likely) doesn't
// already know about. The only case where we'll send a redundant set of
// bytes is if another remote peer gossips to the same peer we're trying to
// gossip to first.
type GossipTracker interface {
	// Tracked returns if a peer is being tracked
	// Returns:
	// 	bool: False if [peerID] is not tracked. True otherwise.
	Tracked(peerID ids.NodeID) bool

	// StartTrackingPeer starts tracking a peer
	// Returns:
	// 	bool: False if [peerID] was already tracked. True otherwise.
	StartTrackingPeer(peerID ids.NodeID) bool
	// StopTrackingPeer stops tracking a given peer
	// Returns:
	// 	bool: False if [peerID] was not tracked. True otherwise.
	StopTrackingPeer(peerID ids.NodeID) bool

	// AddValidator adds a validator that can be gossiped about
	// 	bool: False if [validator] was already present. True otherwise.
	AddValidator(validator ValidatorID) bool
	// RemoveValidator removes a validator that can be gossiped about
	// 	bool: False if [nodeID] was already not present. True otherwise.
	RemoveValidator(validatorID ids.NodeID) bool

	// AddKnown adds [txIDs] to the txIDs known by [peerID]
	// Returns:
	// 	bool: False if [peerID] is not tracked. True otherwise.
	AddKnown(peerID ids.NodeID, txIDs []ids.ID) bool
	// GetUnknown gets the peers that we haven't sent to this peer
	// Returns:
	//	[]ids.NodeID: a slice of [limit] txIDs that [peerID] doesn't know
	//		about.
	// 	bool: False if [peerID] is not tracked. True otherwise.
	GetUnknown(peerID ids.NodeID, limit int) ([]ValidatorID, bool, error)
}

type gossipTracker struct {
	lock sync.RWMutex
	// a mapping of each peer => the validators we have sent them
	trackedPeers map[ids.NodeID]ids.BigBitSet
	// a mapping of validators => the index they occupy in the bitsets
	validatorsToIndices map[ids.NodeID]int
	// each validator in the index it occupies in the bitset
	validators []ValidatorID
	// a mapping of txs => the validators they correspond to
	txsToValidators map[ids.ID]ids.NodeID

	metrics gossipTrackerMetrics
}

// NewGossipTracker returns an instance of gossipTracker
func NewGossipTracker(
	registerer prometheus.Registerer,
	namespace string,
) (GossipTracker, error) {
	m, err := newGossipTrackerMetrics(registerer, fmt.Sprintf("%s_gossip_tracker", namespace))
	if err != nil {
		return nil, err
	}

	return &gossipTracker{
		trackedPeers:        make(map[ids.NodeID]ids.BigBitSet),
		validatorsToIndices: make(map[ids.NodeID]int),
		txsToValidators:     make(map[ids.ID]ids.NodeID),
		metrics:             m,
	}, nil
}

func (g *gossipTracker) Tracked(peerID ids.NodeID) bool {
	g.lock.RLock()
	defer g.lock.RUnlock()

	_, ok := g.trackedPeers[peerID]
	return ok
}

func (g *gossipTracker) StartTrackingPeer(peerID ids.NodeID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	// don't track the peer if it's already being tracked
	if _, ok := g.trackedPeers[peerID]; ok {
		return false
	}

	// start tracking the peer. Initialize their bitset to zero since we
	// haven't sent them anything yet.
	g.trackedPeers[peerID] = ids.NewBigBitSet()

	// emit metrics
	g.metrics.trackedPeersSize.Set(float64(len(g.trackedPeers)))

	return true
}

func (g *gossipTracker) StopTrackingPeer(peerID ids.NodeID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	// only stop tracking peers that are actually being tracked
	if _, ok := g.trackedPeers[peerID]; !ok {
		return false
	}

	// stop tracking the peer by removing them
	delete(g.trackedPeers, peerID)
	g.metrics.trackedPeersSize.Set(float64(len(g.trackedPeers)))

	return true
}

func (g *gossipTracker) AddValidator(validator ValidatorID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	// only add validators that are not already present
	if _, ok := g.validatorsToIndices[validator.NodeID]; ok {
		return false
	}

	if _, ok := g.txsToValidators[validator.TxID]; ok {
		return false
	}

	// add the validator to the MSB of the bitset.
	msb := len(g.validatorsToIndices)
	g.validatorsToIndices[validator.NodeID] = msb
	g.validators = append(g.validators, validator)
	g.txsToValidators[validator.TxID] = validator.NodeID

	// emit metrics
	g.metrics.validatorsToIndicesSize.Set(float64(len(g.validatorsToIndices)))
	g.metrics.validatorIndices.Set(float64(len(g.validators)))

	return true
}

func (g *gossipTracker) RemoveValidator(validatorID ids.NodeID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	// only remove validators that are already present
	indexToRemove, ok := g.validatorsToIndices[validatorID]
	if !ok {
		return false
	}

	// swap the validator-to-be-removed with the validator in the last index
	// if the element we're swapping with is ourselves, we can skip this swap
	// since we only need to delete instead
	lastIndex := len(g.validators) - 1
	if indexToRemove != lastIndex {
		lastPeer := g.validators[lastIndex]

		g.validators[indexToRemove] = lastPeer
		g.validatorsToIndices[lastPeer.NodeID] = indexToRemove
	}

	delete(g.validatorsToIndices, validatorID)
	delete(g.txsToValidators, g.validators[lastIndex].TxID)
	g.validators = g.validators[:lastIndex]

	// invariant: we must remove the validator from everyone else's validator
	// bitsets to make sure that each validator occupies the same position in
	// each bitset.
	for _, knownPeers := range g.trackedPeers {
		// swap the element to be removed with the msb
		if indexToRemove != lastIndex {
			if knownPeers.Contains(lastIndex) {
				knownPeers.Add(indexToRemove)
			} else {
				knownPeers.Remove(indexToRemove)
			}
		}
		knownPeers.Remove(lastIndex)
	}

	// emit metrics
	g.metrics.validatorsToIndicesSize.Set(float64(len(g.validatorsToIndices)))
	g.metrics.validatorIndices.Set(float64(len(g.validators)))

	return true
}

// AddKnown invariants:
//  1. [peerID] SHOULD only be a nodeID that has been tracked with
//     StartTrackingPeer().
//  2. [txIDs] SHOULD only be a slice of txIDs that has been added with
//     AddValidator. Trying to learn about txIDs that aren't registered
//     yet will result in dropping the unregistered ID.
func (g *gossipTracker) AddKnown(peerID ids.NodeID, txIDs []ids.ID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	knownPeers, ok := g.trackedPeers[peerID]
	if !ok {
		return false
	}

	for _, txID := range txIDs {
		nodeID, ok := g.txsToValidators[txID]
		if !ok {
			// We don't know about this txID, this is unexpected.
			continue
		}

		// sanity check that this node we learned about is actually a validator
		idx, ok := g.validatorsToIndices[nodeID]
		if !ok {
			// if we try to learn about a validator that we don't know about,
			// silently continue.
			continue
		}

		knownPeers.Add(idx)
	}

	return true
}

func (g *gossipTracker) GetUnknown(peerID ids.NodeID, limit int) ([]ValidatorID, bool, error) {
	if limit <= 0 {
		return nil, false, nil
	}

	g.lock.RLock()
	defer g.lock.RUnlock()

	// return false if this peer isn't tracked
	knownPeers, ok := g.trackedPeers[peerID]
	if !ok {
		return nil, false, nil
	}

	// We select a random sample of bits to gossip to avoid starving out a
	// validator from being gossiped for ane extended period of time.
	s := sampler.NewUniform()
	if err := s.Initialize(uint64(len(g.validators))); err != nil {
		return nil, false, err
	}

	// Calculate the unknown information we need to send to this peer. We do
	// this by computing the difference between the validators we know about
	// and the validators we know we've sent to [peerID].
	result := make([]ValidatorID, 0, limit)
	for i := 0; i < len(g.validators) && len(result) < limit; i++ {
		drawn, err := s.Next()
		if err != nil {
			return nil, false, err
		}

		if !knownPeers.Contains(int(drawn)) {
			result = append(result, g.validators[drawn])
		}
	}

	return result, true, nil
}

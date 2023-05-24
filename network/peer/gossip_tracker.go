// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
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
	// 	bool: False if a validator with the same node ID or txID as [validator]
	// 	is present. True otherwise.
	AddValidator(validator ValidatorID) bool
	// GetNodeID maps a txID into a nodeIDs
	// 	nodeID: The nodeID that was registered by [txID]
	// 	bool: False if [validator] was not present. True otherwise.
	GetNodeID(txID ids.ID) (ids.NodeID, bool)
	// RemoveValidator removes a validator that can be gossiped about
	// 	bool: False if [validator] was already not present. True otherwise.
	RemoveValidator(validatorID ids.NodeID) bool
	// ResetValidator resets known gossip status of [validatorID] to unknown
	// for all peers
	// 	bool: False if [validator] was not present. True otherwise.
	ResetValidator(validatorID ids.NodeID) bool

	// AddKnown adds [knownTxIDs] to the txIDs known by [peerID] and filters
	// [txIDs] for non-validators.
	// Returns:
	// 	txIDs: The txIDs in [txIDs] that are currently validators.
	// 	bool: False if [peerID] is not tracked. True otherwise.
	AddKnown(
		peerID ids.NodeID,
		knownTxIDs []ids.ID,
		txIDs []ids.ID,
	) ([]ids.ID, bool)
	// GetUnknown gets the peers that we haven't sent to this peer
	// Returns:
	// 	[]ValidatorID: a slice of ValidatorIDs that [peerID] doesn't know about.
	// 	bool: False if [peerID] is not tracked. True otherwise.
	GetUnknown(peerID ids.NodeID) ([]ValidatorID, bool)
}

type gossipTracker struct {
	lock sync.RWMutex
	// a mapping of txIDs => the validator added to the validiator set by that
	// tx.
	txIDsToNodeIDs map[ids.ID]ids.NodeID
	// a mapping of validators => the index they occupy in the bitsets
	nodeIDsToIndices map[ids.NodeID]int
	// each validator in the index it occupies in the bitset
	validatorIDs []ValidatorID
	// a mapping of each peer => the validators they know about
	trackedPeers map[ids.NodeID]set.Bits

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
		txIDsToNodeIDs:   make(map[ids.ID]ids.NodeID),
		nodeIDsToIndices: make(map[ids.NodeID]int),
		trackedPeers:     make(map[ids.NodeID]set.Bits),
		metrics:          m,
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
	g.trackedPeers[peerID] = set.NewBits()

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
	if _, ok := g.txIDsToNodeIDs[validator.TxID]; ok {
		return false
	}
	if _, ok := g.nodeIDsToIndices[validator.NodeID]; ok {
		return false
	}

	// add the validator to the MSB of the bitset.
	msb := len(g.validatorIDs)
	g.txIDsToNodeIDs[validator.TxID] = validator.NodeID
	g.nodeIDsToIndices[validator.NodeID] = msb
	g.validatorIDs = append(g.validatorIDs, validator)

	// emit metrics
	g.metrics.validatorsSize.Set(float64(len(g.validatorIDs)))

	return true
}

func (g *gossipTracker) GetNodeID(txID ids.ID) (ids.NodeID, bool) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	nodeID, ok := g.txIDsToNodeIDs[txID]
	return nodeID, ok
}

func (g *gossipTracker) RemoveValidator(validatorID ids.NodeID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	// only remove validators that are already present
	indexToRemove, ok := g.nodeIDsToIndices[validatorID]
	if !ok {
		return false
	}
	validatorToRemove := g.validatorIDs[indexToRemove]

	// swap the validator-to-be-removed with the validator in the last index
	// if the element we're swapping with is ourselves, we can skip this swap
	// since we only need to delete instead
	lastIndex := len(g.validatorIDs) - 1
	if indexToRemove != lastIndex {
		lastValidator := g.validatorIDs[lastIndex]

		g.nodeIDsToIndices[lastValidator.NodeID] = indexToRemove
		g.validatorIDs[indexToRemove] = lastValidator
	}

	delete(g.txIDsToNodeIDs, validatorToRemove.TxID)
	delete(g.nodeIDsToIndices, validatorID)
	g.validatorIDs = g.validatorIDs[:lastIndex]

	// Invariant: We must remove the validator from everyone else's validator
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
	g.metrics.validatorsSize.Set(float64(len(g.validatorIDs)))

	return true
}

func (g *gossipTracker) ResetValidator(validatorID ids.NodeID) bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	// only reset validators that exist
	indexToReset, ok := g.nodeIDsToIndices[validatorID]
	if !ok {
		return false
	}

	for _, knownPeers := range g.trackedPeers {
		knownPeers.Remove(indexToReset)
	}

	return true
}

// AddKnown invariants:
//
//  1. [peerID] SHOULD only be a nodeID that has been tracked with
//     StartTrackingPeer().
func (g *gossipTracker) AddKnown(
	peerID ids.NodeID,
	knownTxIDs []ids.ID,
	txIDs []ids.ID,
) ([]ids.ID, bool) {
	g.lock.Lock()
	defer g.lock.Unlock()

	knownPeers, ok := g.trackedPeers[peerID]
	if !ok {
		return nil, false
	}
	for _, txID := range knownTxIDs {
		nodeID, ok := g.txIDsToNodeIDs[txID]
		if !ok {
			// We don't know about this txID, this can happen due to differences
			// between our current validator set and the peer's current
			// validator set.
			continue
		}

		// Because we fetched the nodeID from [g.txIDsToNodeIDs], we are
		// guaranteed that the index is populated.
		index := g.nodeIDsToIndices[nodeID]
		knownPeers.Add(index)
	}

	validatorTxIDs := make([]ids.ID, 0, len(txIDs))
	for _, txID := range txIDs {
		if _, ok := g.txIDsToNodeIDs[txID]; ok {
			validatorTxIDs = append(validatorTxIDs, txID)
		}
	}
	return validatorTxIDs, true
}

func (g *gossipTracker) GetUnknown(peerID ids.NodeID) ([]ValidatorID, bool) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	// return false if this peer isn't tracked
	knownPeers, ok := g.trackedPeers[peerID]
	if !ok {
		return nil, false
	}

	// Calculate the unknown information we need to send to this peer. We do
	// this by computing the difference between the validators we know about
	// and the validators we know we've sent to [peerID].
	result := make([]ValidatorID, 0, len(g.validatorIDs))
	for i, validatorID := range g.validatorIDs {
		if !knownPeers.Contains(i) {
			result = append(result, validatorID)
		}
	}

	return result, true
}

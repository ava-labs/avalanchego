// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"context"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	_ Peers = (*lockedPeers)(nil)
	_ Peers = (*meteredPeers)(nil)
	_ Peers = (*peerData)(nil)
)

type Peers interface {
	validators.SetCallbackListener
	validators.Connector

	// ConnectedWeight returns the currently connected stake weight
	ConnectedWeight() uint64
	// ConnectedPercent returns the currently connected stake percentage [0, 1]
	ConnectedPercent() float64
	// PreferredPeers returns the currently connected validators. If there are
	// no currently connected validators then it will return the currently
	// connected peers.
	PreferredPeers() set.Set[ids.NodeID]
}

type lockedPeers struct {
	lock  sync.RWMutex
	peers Peers
}

func NewPeers() Peers {
	return &lockedPeers{
		peers: &peerData{
			validators: make(map[ids.GenericNodeID]uint64),
		},
	}
}

func (p *lockedPeers) OnValidatorAdded(nodeID ids.GenericNodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.peers.OnValidatorAdded(nodeID, pk, txID, weight)
}

func (p *lockedPeers) OnValidatorRemoved(nodeID ids.GenericNodeID, weight uint64) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.peers.OnValidatorRemoved(nodeID, weight)
}

func (p *lockedPeers) OnValidatorWeightChanged(nodeID ids.GenericNodeID, oldWeight, newWeight uint64) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.peers.OnValidatorWeightChanged(nodeID, oldWeight, newWeight)
}

func (p *lockedPeers) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	return p.peers.Connected(ctx, nodeID, version)
}

func (p *lockedPeers) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	return p.peers.Disconnected(ctx, nodeID)
}

func (p *lockedPeers) ConnectedWeight() uint64 {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.peers.ConnectedWeight()
}

func (p *lockedPeers) ConnectedPercent() float64 {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.peers.ConnectedPercent()
}

func (p *lockedPeers) PreferredPeers() set.Set[ids.NodeID] {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.peers.PreferredPeers()
}

type meteredPeers struct {
	Peers

	percentConnected prometheus.Gauge
}

func NewMeteredPeers(namespace string, reg prometheus.Registerer) (Peers, error) {
	percentConnected := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "percent_connected",
		Help:      "Percent of connected stake",
	})
	return &lockedPeers{
		peers: &meteredPeers{
			Peers: &peerData{
				validators: make(map[ids.GenericNodeID]uint64),
			},
			percentConnected: percentConnected,
		},
	}, reg.Register(percentConnected)
}

func (p *meteredPeers) OnValidatorAdded(nodeID ids.GenericNodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	p.Peers.OnValidatorAdded(nodeID, pk, txID, weight)
	p.percentConnected.Set(p.Peers.ConnectedPercent())
}

func (p *meteredPeers) OnValidatorRemoved(nodeID ids.GenericNodeID, weight uint64) {
	p.Peers.OnValidatorRemoved(nodeID, weight)
	p.percentConnected.Set(p.Peers.ConnectedPercent())
}

func (p *meteredPeers) OnValidatorWeightChanged(nodeID ids.GenericNodeID, oldWeight, newWeight uint64) {
	p.Peers.OnValidatorWeightChanged(nodeID, oldWeight, newWeight)
	p.percentConnected.Set(p.Peers.ConnectedPercent())
}

func (p *meteredPeers) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	err := p.Peers.Connected(ctx, nodeID, version)
	p.percentConnected.Set(p.Peers.ConnectedPercent())
	return err
}

func (p *meteredPeers) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	err := p.Peers.Disconnected(ctx, nodeID)
	p.percentConnected.Set(p.Peers.ConnectedPercent())
	return err
}

type peerData struct {
	// validators maps nodeIDs to their current stake weight
	validators map[ids.GenericNodeID]uint64
	// totalWeight is the total weight of all validators
	totalWeight uint64
	// connectedWeight contains the sum of all connected validator weights
	connectedWeight uint64
	// connectedValidators is the set of currently connected peers with a
	// non-zero stake weight
	connectedValidators set.Set[ids.GenericNodeID]
	// connectedPeers is the set of all connected peers
	connectedPeers set.Set[ids.GenericNodeID]
}

func (p *peerData) OnValidatorAdded(nodeID ids.GenericNodeID, _ *bls.PublicKey, _ ids.ID, weight uint64) {
	p.validators[nodeID] = weight
	p.totalWeight += weight
	if p.connectedPeers.Contains(nodeID) {
		p.connectedWeight += weight
		p.connectedValidators.Add(nodeID)
	}
}

func (p *peerData) OnValidatorRemoved(nodeID ids.GenericNodeID, weight uint64) {
	delete(p.validators, nodeID)
	p.totalWeight -= weight
	if p.connectedPeers.Contains(nodeID) {
		p.connectedWeight -= weight
		p.connectedValidators.Remove(nodeID)
	}
}

func (p *peerData) OnValidatorWeightChanged(nodeID ids.GenericNodeID, oldWeight, newWeight uint64) {
	p.validators[nodeID] = newWeight
	p.totalWeight -= oldWeight
	p.totalWeight += newWeight
	if p.connectedPeers.Contains(nodeID) {
		p.connectedWeight -= oldWeight
		p.connectedWeight += newWeight
	}
}

func (p *peerData) Connected(_ context.Context, nodeID ids.NodeID, _ *version.Application) error {
	genericNodeID := ids.GenericNodeIDFromNodeID(nodeID)
	if weight, ok := p.validators[genericNodeID]; ok {
		p.connectedWeight += weight
		p.connectedValidators.Add(genericNodeID)
	}
	p.connectedPeers.Add(genericNodeID)
	return nil
}

func (p *peerData) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	genericNodeID := ids.GenericNodeIDFromNodeID(nodeID)
	if weight, ok := p.validators[genericNodeID]; ok {
		p.connectedWeight -= weight
		p.connectedValidators.Remove(genericNodeID)
	}
	p.connectedPeers.Remove(genericNodeID)
	return nil
}

func (p *peerData) ConnectedWeight() uint64 {
	return p.connectedWeight
}

func (p *peerData) ConnectedPercent() float64 {
	if p.totalWeight == 0 {
		return 1
	}
	return float64(p.connectedWeight) / float64(p.totalWeight)
}

func (p *peerData) PreferredPeers() set.Set[ids.NodeID] {
	if p.connectedValidators.Len() == 0 {
		connectedPeers := set.NewSet[ids.NodeID](p.connectedPeers.Len())
		for genericPeer := range p.connectedPeers {
			peer, err := ids.NodeIDFromGenericNodeID(genericPeer)
			if err != nil {
				panic(err)
			}
			connectedPeers.Add(peer)
		}
		return connectedPeers
	}

	connectedValidators := set.NewSet[ids.NodeID](p.connectedValidators.Len())
	for genericPeer := range p.connectedValidators {
		peer, err := ids.NodeIDFromGenericNodeID(genericPeer)
		if err != nil {
			panic(err)
		}
		connectedValidators.Add(peer)
	}
	return connectedValidators
}

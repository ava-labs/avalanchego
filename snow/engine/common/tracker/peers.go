// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"context"
	"errors"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/exp/maps"

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
	// SampleValidator returns a randomly selected connected validator. If there
	// are no currently connected validators then it will return false.
	SampleValidator() (ids.NodeID, bool)
	// GetValidators returns the set of all validators
	// known to this peer manager
	GetValidators() set.Set[ids.NodeID]
	// ConnectedValidators returns the set of all validators
	// that are currently connected
	ConnectedValidators() set.Set[ids.NodeID]
}

type lockedPeers struct {
	lock  sync.RWMutex
	peers Peers
}

func NewPeers() Peers {
	return &lockedPeers{
		peers: &peerData{
			validators: make(map[ids.NodeID]uint64),
		},
	}
}

func (p *lockedPeers) OnValidatorAdded(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.peers.OnValidatorAdded(nodeID, pk, txID, weight)
}

func (p *lockedPeers) OnValidatorRemoved(nodeID ids.NodeID, weight uint64) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.peers.OnValidatorRemoved(nodeID, weight)
}

func (p *lockedPeers) OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64) {
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

func (p *lockedPeers) SampleValidator() (ids.NodeID, bool) {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.peers.SampleValidator()
}

func (p *lockedPeers) GetValidators() set.Set[ids.NodeID] {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.peers.GetValidators()
}

func (p *lockedPeers) ConnectedValidators() set.Set[ids.NodeID] {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.peers.ConnectedValidators()
}

type meteredPeers struct {
	Peers

	percentConnected prometheus.Gauge
	numValidators    prometheus.Gauge
	totalWeight      prometheus.Gauge
}

func NewMeteredPeers(reg prometheus.Registerer) (Peers, error) {
	percentConnected := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "percent_connected",
		Help: "Percent of connected stake",
	})
	totalWeight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "total_weight",
		Help: "Total stake",
	})
	numValidators := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "num_validators",
		Help: "Total number of validators",
	})
	err := errors.Join(
		reg.Register(percentConnected),
		reg.Register(totalWeight),
		reg.Register(numValidators),
	)
	return &lockedPeers{
		peers: &meteredPeers{
			Peers: &peerData{
				validators: make(map[ids.NodeID]uint64),
			},
			percentConnected: percentConnected,
			totalWeight:      totalWeight,
			numValidators:    numValidators,
		},
	}, err
}

func (p *meteredPeers) OnValidatorAdded(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	p.Peers.OnValidatorAdded(nodeID, pk, txID, weight)
	p.numValidators.Inc()
	p.totalWeight.Add(float64(weight))
	p.percentConnected.Set(p.Peers.ConnectedPercent())
}

func (p *meteredPeers) OnValidatorRemoved(nodeID ids.NodeID, weight uint64) {
	p.Peers.OnValidatorRemoved(nodeID, weight)
	p.numValidators.Dec()
	p.totalWeight.Sub(float64(weight))
	p.percentConnected.Set(p.Peers.ConnectedPercent())
}

func (p *meteredPeers) OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64) {
	p.Peers.OnValidatorWeightChanged(nodeID, oldWeight, newWeight)
	p.totalWeight.Sub(float64(oldWeight))
	p.totalWeight.Add(float64(newWeight))
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
	validators map[ids.NodeID]uint64
	// totalWeight is the total weight of all validators
	totalWeight uint64
	// connectedWeight contains the sum of all connected validator weights
	connectedWeight uint64
	// connectedValidators is the set of currently connected peers with a
	// non-zero stake weight
	connectedValidators set.Set[ids.NodeID]
	// connectedPeers is the set of all connected peers
	connectedPeers set.Set[ids.NodeID]
}

func (p *peerData) OnValidatorAdded(nodeID ids.NodeID, _ *bls.PublicKey, _ ids.ID, weight uint64) {
	p.validators[nodeID] = weight
	p.totalWeight += weight
	if p.connectedPeers.Contains(nodeID) {
		p.connectedWeight += weight
		p.connectedValidators.Add(nodeID)
	}
}

func (p *peerData) OnValidatorRemoved(nodeID ids.NodeID, weight uint64) {
	delete(p.validators, nodeID)
	p.totalWeight -= weight
	if p.connectedPeers.Contains(nodeID) {
		p.connectedWeight -= weight
		p.connectedValidators.Remove(nodeID)
	}
}

func (p *peerData) OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64) {
	p.validators[nodeID] = newWeight
	p.totalWeight -= oldWeight
	p.totalWeight += newWeight
	if p.connectedPeers.Contains(nodeID) {
		p.connectedWeight -= oldWeight
		p.connectedWeight += newWeight
	}
}

func (p *peerData) Connected(_ context.Context, nodeID ids.NodeID, _ *version.Application) error {
	if weight, ok := p.validators[nodeID]; ok {
		p.connectedWeight += weight
		p.connectedValidators.Add(nodeID)
	}
	p.connectedPeers.Add(nodeID)
	return nil
}

func (p *peerData) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	if weight, ok := p.validators[nodeID]; ok {
		p.connectedWeight -= weight
		p.connectedValidators.Remove(nodeID)
	}
	p.connectedPeers.Remove(nodeID)
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

func (p *peerData) SampleValidator() (ids.NodeID, bool) {
	return p.connectedValidators.Peek()
}

func (p *peerData) GetValidators() set.Set[ids.NodeID] {
	return set.Of(maps.Keys(p.validators)...)
}

func (p *peerData) ConnectedValidators() set.Set[ids.NodeID] {
	// The set is copied to avoid future changes from being reflected in the
	// returned set.
	copied := set.NewSet[ids.NodeID](len(p.connectedValidators))
	copied.Union(p.connectedValidators)
	return copied
}

// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var _ Peers = (*peers)(nil)

type Peers interface {
	validators.SetCallbackListener
	validators.Connector

	// ConnectedWeight returns the currently connected stake weight
	ConnectedWeight() uint64
	// PreferredPeers returns the currently connected validators. If there are
	// no currently connected validators then it will return the currently
	// connected peers.
	PreferredPeers() set.Set[ids.NodeID]
}

type peers struct {
	lock sync.RWMutex
	// validators maps nodeIDs to their current stake weight
	validators map[ids.NodeID]uint64
	// connectedWeight contains the sum of all connected validator weights
	connectedWeight uint64
	// connectedValidators is the set of currently connected peers with a
	// non-zero stake weight
	connectedValidators set.Set[ids.NodeID]
	// connectedPeers is the set of all connected peers
	connectedPeers set.Set[ids.NodeID]
}

func NewPeers() Peers {
	return &peers{
		validators: make(map[ids.NodeID]uint64),
	}
}

func (p *peers) OnValidatorAdded(nodeID ids.NodeID, _ *bls.PublicKey, _ ids.ID, weight uint64) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.validators[nodeID] = weight
	if p.connectedPeers.Contains(nodeID) {
		p.connectedWeight += weight
		p.connectedValidators.Add(nodeID)
	}
}

func (p *peers) OnValidatorRemoved(nodeID ids.NodeID, weight uint64) {
	p.lock.Lock()
	defer p.lock.Unlock()

	delete(p.validators, nodeID)
	if p.connectedPeers.Contains(nodeID) {
		p.connectedWeight -= weight
		p.connectedValidators.Remove(nodeID)
	}
}

func (p *peers) OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.validators[nodeID] = newWeight
	if p.connectedPeers.Contains(nodeID) {
		p.connectedWeight -= oldWeight
		p.connectedWeight += newWeight
	}
}

func (p *peers) Connected(_ context.Context, nodeID ids.NodeID, _ *version.Application) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	if weight, ok := p.validators[nodeID]; ok {
		p.connectedWeight += weight
		p.connectedValidators.Add(nodeID)
	}
	p.connectedPeers.Add(nodeID)
	return nil
}

func (p *peers) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	if weight, ok := p.validators[nodeID]; ok {
		p.connectedWeight -= weight
		p.connectedValidators.Remove(nodeID)
	}
	p.connectedPeers.Remove(nodeID)
	return nil
}

func (p *peers) ConnectedWeight() uint64 {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.connectedWeight
}

func (p *peers) PreferredPeers() set.Set[ids.NodeID] {
	p.lock.RLock()
	defer p.lock.RUnlock()

	if p.connectedValidators.Len() == 0 {
		connectedPeers := set.NewSet[ids.NodeID](p.connectedPeers.Len())
		connectedPeers.Union(p.connectedPeers)
		return connectedPeers
	}

	connectedValidators := set.NewSet[ids.NodeID](p.connectedValidators.Len())
	connectedValidators.Union(p.connectedValidators)
	return connectedValidators
}

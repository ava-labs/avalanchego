// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	_ validators.Connector = (*Peers)(nil)
	_ NodeSampler          = (*Peers)(nil)
)

// Peers contains a set of nodes that we are connected to.
type Peers struct {
	lock  sync.RWMutex
	peers set.SampleableSet[ids.NodeID]
}

func (p *Peers) Connected(_ context.Context, nodeID ids.NodeID, _ *version.Application) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.peers.Add(nodeID)

	return nil
}

func (p *Peers) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.peers.Remove(nodeID)

	return nil
}

func (p *Peers) Sample(_ context.Context, limit int) []ids.NodeID {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.peers.Sample(limit)
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/version"
)

var _ Startup = (*startup)(nil)

type Startup interface {
	Peers

	ShouldStart() bool
}

type startup struct {
	Peers

	lock          sync.RWMutex
	startupWeight uint64
	shouldStart   bool
}

func NewStartup(peers Peers, startupWeight uint64) Startup {
	return &startup{
		Peers:         peers,
		startupWeight: startupWeight,
		shouldStart:   peers.ConnectedWeight() >= startupWeight,
	}
}

func (s *startup) OnValidatorAdded(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.Peers.OnValidatorAdded(nodeID, pk, txID, weight)
	s.shouldStart = s.shouldStart || s.Peers.ConnectedWeight() >= s.startupWeight
}

func (s *startup) OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.Peers.OnValidatorWeightChanged(nodeID, oldWeight, newWeight)
	s.shouldStart = s.shouldStart || s.Peers.ConnectedWeight() >= s.startupWeight
}

func (s *startup) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.Peers.Connected(ctx, nodeID, nodeVersion); err != nil {
		return err
	}

	s.shouldStart = s.shouldStart || s.Peers.ConnectedWeight() >= s.startupWeight
	return nil
}

func (s *startup) ShouldStart() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.shouldStart
}

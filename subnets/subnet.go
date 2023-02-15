// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ Subnet = (*subnet)(nil)

// Subnet keeps track of the currently bootstrapping chains in a subnet. If no
// chains in the subnet are currently bootstrapping, the subnet is considered
// bootstrapped.
type Subnet interface {
	common.BootstrapTracker

	// AddChain adds a chain to this Subnet
	AddChain(chainID ids.ID) bool
	// Config returns config of this Subnet
	Config() Config
}

type subnet struct {
	lock             sync.RWMutex
	bootstrapping    set.Set[ids.ID]
	bootstrapped     set.Set[ids.ID]
	once             sync.Once
	bootstrappedSema chan struct{}
	config           Config
}

func New(config Config) Subnet {
	return &subnet{
		bootstrappedSema: make(chan struct{}),
		config:           config,
	}
}

func (s *subnet) IsBootstrapped() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.bootstrapping.Len() == 0
}

func (s *subnet) Bootstrapped(chainID ids.ID) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.bootstrapping.Remove(chainID)
	s.bootstrapped.Add(chainID)
	if s.bootstrapping.Len() > 0 {
		return
	}

	s.once.Do(func() {
		close(s.bootstrappedSema)
	})
}

func (s *subnet) OnBootstrapCompleted() chan struct{} {
	return s.bootstrappedSema
}

func (s *subnet) AddChain(chainID ids.ID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.bootstrapping.Contains(chainID) || s.bootstrapped.Contains(chainID) {
		return false
	}

	s.bootstrapping.Add(chainID)
	return true
}

func (s *subnet) Config() Config {
	return s.config
}

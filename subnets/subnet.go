// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

var _ Subnet = (*subnet)(nil)

type Allower interface {
	// IsAllowed filters out nodes that are not allowed to connect to this subnet
	IsAllowed(nodeID ids.NodeID, isValidator bool) bool
}

// Subnet keeps track of the currently bootstrapping chains in a subnet. If no
// chains in the subnet are currently bootstrapping, the subnet is considered
// bootstrapped.
type Subnet interface {
	StateTracker

	// AddChain adds a chain to this Subnet
	AddChain(chainID ids.ID) bool

	// Config returns config of this Subnet
	Config() Config

	Allower
}

type subnet struct {
	lock sync.RWMutex

	chainToState map[ids.ID]snow.State

	once             sync.Once
	bootstrappedSema chan struct{}
	config           Config
	myNodeID         ids.NodeID
}

func New(myNodeID ids.NodeID, config Config) Subnet {
	return &subnet{
		chainToState:     make(map[ids.ID]snow.State),
		bootstrappedSema: make(chan struct{}),
		config:           config,
		myNodeID:         myNodeID,
	}
}

func (s *subnet) IsSynced() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.isSynced()
}

// isSynced assumes s.lock is held
func (s *subnet) isSynced() bool {
	synced := true
	for _, st := range s.chainToState {
		if st != snow.NormalOp {
			synced = false
			break
		}
	}
	return synced
}

func (s *subnet) SetState(chainID ids.ID, state snow.State) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.chainToState[chainID] = state
	if !s.isSynced() {
		return
	}

	s.once.Do(func() {
		close(s.bootstrappedSema)
	})
}

func (s *subnet) GetState(chainID ids.ID) snow.State {
	s.lock.Lock()
	defer s.lock.Unlock()

	return snow.Initializing
}

func (s *subnet) OnSyncCompleted() chan struct{} {
	return s.bootstrappedSema
}

func (s *subnet) AddChain(chainID ids.ID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, found := s.chainToState[chainID]; found {
		return false
	}

	s.chainToState[chainID] = snow.Initializing
	return true
}

func (s *subnet) Config() Config {
	return s.config
}

func (s *subnet) IsAllowed(nodeID ids.NodeID, isValidator bool) bool {
	// Case 1: NodeID is this node
	// Case 2: This subnet is not validator-only subnet
	// Case 3: NodeID is a validator for this chain
	// Case 4: NodeID is explicitly allowed whether it's subnet validator or not
	return nodeID == s.myNodeID ||
		!s.config.ValidatorOnly ||
		isValidator ||
		s.config.AllowedNodes.Contains(nodeID)
}

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
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
	snow.SubnetStateTracker

	// AddChain adds a chain to this Subnet
	AddChain(chainID ids.ID) bool

	// Config returns config of this Subnet
	Config() Config

	Allower
}

type subnet struct {
	lock sync.RWMutex

	currentState map[ids.ID]snow.State
	started      map[snow.State]set.Set[ids.ID]
	stopped      map[snow.State]set.Set[ids.ID]

	once             sync.Once
	bootstrappedSema chan struct{}
	config           Config
	myNodeID         ids.NodeID
}

func New(myNodeID ids.NodeID, config Config) Subnet {
	return &subnet{
		currentState:     make(map[ids.ID]snow.State),
		started:          make(map[snow.State]set.Set[ids.ID]),
		stopped:          make(map[snow.State]set.Set[ids.ID]),
		bootstrappedSema: make(chan struct{}),
		config:           config,
		myNodeID:         myNodeID,
	}
}

func (s *subnet) IsSubnetSynced() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.isSynced()
}

// isSynced assumes s.lock is held
// Currently a subnet is declared synced if
// all chains have completed bootstrap, even if chains VM
// have not yet started normal operations
func (s *subnet) isSynced() bool {
	bootstrapped, found := s.stopped[snow.Bootstrapping]
	if !found {
		// no chain completed bootstrapping
		return false
	}

	synced := true
	for chain := range s.currentState {
		if !bootstrapped.Contains(chain) {
			synced = false
			break
		}
	}
	return synced
}

func (s *subnet) StartState(chainID ids.ID, state snow.State) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.currentState[chainID] = state
	started, found := s.started[state]
	if !found {
		s.started[state] = set.NewSet[ids.ID](3)
		started = s.started[state]
	}
	started.Add(chainID)
	if !s.isSynced() {
		return
	}

	s.once.Do(func() {
		close(s.bootstrappedSema)
	})
}

func (s *subnet) StopState(chainID ids.ID, state snow.State) {
	s.lock.Lock()
	defer s.lock.Unlock()

	stopped, found := s.stopped[state]
	if !found {
		s.stopped[state] = set.NewSet[ids.ID](3)
		stopped = s.stopped[state]
	}
	stopped.Add(chainID)
}

func (s *subnet) GetState(chainID ids.ID) snow.State {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.currentState[chainID]
}

func (s *subnet) OnSyncCompleted() chan struct{} {
	return s.bootstrappedSema
}

func (s *subnet) AddChain(chainID ids.ID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, found := s.currentState[chainID]; found {
		return false
	}

	s.currentState[chainID] = snow.Initializing
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

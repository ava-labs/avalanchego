// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
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

	currentEngineType p2p.EngineType

	// currentState maps chainID to chain's latest started state
	currentState map[ids.ID]snow.State

	// started maps a vm state to the set of VMs that has started that state
	started map[snow.State]set.Set[ids.ID]

	// done maps a vm state to the set of VMs that has done with that state
	done map[snow.State]set.Set[ids.ID]

	onces      map[ids.ID]*sync.Once
	semaphores map[ids.ID]chan struct{}
	config     Config
	myNodeID   ids.NodeID
}

func New(myNodeID ids.NodeID, config Config) Subnet {
	return &subnet{
		currentState: make(map[ids.ID]snow.State),
		started:      make(map[snow.State]set.Set[ids.ID]),
		done:         make(map[snow.State]set.Set[ids.ID]),
		onces:        make(map[ids.ID]*sync.Once),
		semaphores:   make(map[ids.ID]chan struct{}),
		config:       config,
		myNodeID:     myNodeID,
	}
}

func (s *subnet) IsSynced() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.isSynced()
}

// isSynced assumes s.lock is held
func (s *subnet) isSynced() bool {
	bootstrapped, anyChainDoneBootstrap := s.done[snow.Bootstrapping]
	if !anyChainDoneBootstrap {
		// Note: a subnet with no registered chains is marked as not synced.
		// This choice avoid the subnet flipping between synced and not synced
		// in case chains are added at a later time.
		return false
	}

	stateSyncedStarted, anyChainStartedStateSync := s.started[snow.StateSyncing]
	stateSyncedDone, anyChainDoneStateSync := s.done[snow.StateSyncing]

	if anyChainStartedStateSync && !anyChainDoneStateSync {
		// some chains have started state sync but none have finished it.
		// Can't be fully synced
		return false
	}

	synced := true
	for chain := range s.currentState {
		// full sync requires bootstrapping done
		if !bootstrapped.Contains(chain) {
			synced = false
			break
		}

		// full sync requires state sync done, if ever started
		if stateSyncedStarted.Contains(chain) &&
			(stateSyncedDone != nil && !stateSyncedDone.Contains(chain)) {
			synced = false
			break
		}
	}
	return synced
}

func (s *subnet) IsChainBootstrapped(chainID ids.ID) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	// chain must have completed bootstrap
	doneBootstrap := s.done[snow.Bootstrapping]
	if !doneBootstrap.Contains(chainID) {
		return false
	}

	// chain must have complete state sync only if it ever started
	startedStateSync := s.started[snow.StateSyncing]
	if !startedStateSync.Contains(chainID) {
		// bootstrap done, state sync never started
		return true
	}

	// state sync started, must have finished
	stoppedStateSync := s.done[snow.StateSyncing]
	return stoppedStateSync.Contains(chainID)
}

func (s *subnet) GetState(chainID ids.ID) (snow.State, p2p.EngineType) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.currentState[chainID], s.currentEngineType
}

func (s *subnet) StartState(chainID ids.ID, state snow.State, currentEngineType p2p.EngineType) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.currentEngineType = currentEngineType

	s.currentState[chainID] = state
	started, anyChainStarted := s.started[state]
	if !anyChainStarted {
		s.started[state] = set.NewSet[ids.ID](3)
		started = s.started[state]
	}
	started.Add(chainID)

	// if we are restarting a given state, make sure it is not marked as stopped
	if stopped, anyChainStopped := s.done[state]; anyChainStopped {
		stopped.Remove(chainID)
	}

	if !s.isSynced() {
		return
	}

	for chainID, ch := range s.semaphores {
		once := s.onces[chainID]
		once.Do(func() {
			close(ch)
		})
	}
}

func (s *subnet) StopState(chainID ids.ID, state snow.State) {
	s.lock.Lock()
	defer s.lock.Unlock()

	stopped, anyChainDoneBootstrap := s.done[state]
	if !anyChainDoneBootstrap {
		s.done[state] = set.NewSet[ids.ID](3)
		stopped = s.done[state]
	}
	stopped.Add(chainID)

	if !s.isSynced() {
		return
	}
	for chainID, ch := range s.semaphores {
		once := s.onces[chainID]
		once.Do(func() {
			close(ch)
		})
	}
}

func (s *subnet) OnSyncCompleted(chainID ids.ID) (chan struct{}, error) {
	if _, found := s.semaphores[chainID]; !found {
		return nil, fmt.Errorf("unknown chain %s", chainID)
	}

	return s.semaphores[chainID], nil
}

func (s *subnet) AddChain(chainID ids.ID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, alreadyAdded := s.currentState[chainID]; alreadyAdded {
		return false
	}

	s.currentState[chainID] = snow.Initializing
	s.onces[chainID] = &sync.Once{}
	s.semaphores[chainID] = make(chan struct{})
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

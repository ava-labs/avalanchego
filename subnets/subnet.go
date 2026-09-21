// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ Subnet = (*subnet)(nil)

type Allower interface {
	// IsAllowed filters out nodes that are not allowed to connect to this subnet
	IsAllowed(nodeID ids.NodeID) bool
}

// MembershipChecker reports whether a peer is a member of a subnet: a validator
// of it, a peer whose staking certificate chains to its member CA, or a peer
// listed in its allowedNodes. It is implemented by the network layer, which is
// where all three are resolved.
type MembershipChecker interface {
	IsSubnetMember(subnetID ids.ID, nodeID ids.NodeID) bool
}

// NoOpMembershipChecker reports no peer as a member. Tests pass it to [New]
// when membership is irrelevant; production always supplies a real checker.
var NoOpMembershipChecker MembershipChecker = noOpMembershipChecker{}

type noOpMembershipChecker struct{}

func (noOpMembershipChecker) IsSubnetMember(ids.ID, ids.NodeID) bool {
	return false
}

// Subnet keeps track of the currently bootstrapping chains in a subnet. If no
// chains in the subnet are currently bootstrapping, the subnet is considered
// bootstrapped.
type Subnet interface {
	common.BootstrapTracker

	// AddChain adds a chain to this Subnet
	AddChain(chainID ids.ID) bool

	// Config returns config of this Subnet
	Config() Config

	Allower
}

type subnet struct {
	lock            sync.RWMutex
	bootstrapping   set.Set[ids.ID]
	bootstrapped    set.Set[ids.ID]
	config          Config
	subnetID        ids.ID
	myNodeID        ids.NodeID
	members         MembershipChecker
	bootstrapSignal common.PreemptionSignal
}

// New returns the Subnet [subnetID] as this node runs it. [members] reports
// which peers are members of it.
func New(
	myNodeID ids.NodeID,
	subnetID ids.ID,
	config Config,
	members MembershipChecker,
) Subnet {
	return &subnet{
		config:   config,
		subnetID: subnetID,
		myNodeID: myNodeID,
		members:  members,
	}
}

func (s *subnet) AllBootstrapped() <-chan struct{} {
	return s.bootstrapSignal.Listen()
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

	s.bootstrapSignal.Preempt()
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

func (s *subnet) IsAllowed(nodeID ids.NodeID) bool {
	// Case 1: NodeID is this node
	// Case 2: This subnet is not a validator-only subnet
	// Case 3: NodeID is a member of this subnet
	return nodeID == s.myNodeID ||
		!s.config.ValidatorOnly ||
		s.members.IsSubnetMember(s.subnetID, nodeID)
}

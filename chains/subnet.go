// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ Subnet = (*subnet)(nil)

// Subnet keeps track of the currently bootstrapping chains in a subnet. If no
// chains in the subnet are currently bootstrapping, the subnet is considered
// bootstrapped.
type Subnet interface {
	common.Subnet

	afterBootstrapped() chan struct{}

	addChain(chainID ids.ID) bool
}

type SubnetConfig struct {
	sender.GossipConfig

	// ValidatorOnly indicates that this Subnet's Chains are available to only subnet validators.
	ValidatorOnly       bool                 `json:"validatorOnly" yaml:"validatorOnly"`
	ConsensusParameters avalanche.Parameters `json:"consensusParameters" yaml:"consensusParameters"`

	// ProposerMinBlockDelay is the minimum delay this node will enforce when
	// building a snowman++ block.
	// TODO: Remove this flag once all VMs throttle their own block production.
	ProposerMinBlockDelay time.Duration `json:"proposerMinBlockDelay" yaml:"proposerMinBlockDelay"`
}

type subnet struct {
	lock             sync.RWMutex
	bootstrapping    set.Set[ids.ID]
	bootstrapped     set.Set[ids.ID]
	once             sync.Once
	bootstrappedSema chan struct{}
}

func newSubnet() Subnet {
	return &subnet{
		bootstrappedSema: make(chan struct{}),
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

func (s *subnet) afterBootstrapped() chan struct{} {
	return s.bootstrappedSema
}

func (s *subnet) addChain(chainID ids.ID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.bootstrapping.Contains(chainID) || s.bootstrapped.Contains(chainID) {
		return false
	}

	s.bootstrapping.Add(chainID)
	return true
}

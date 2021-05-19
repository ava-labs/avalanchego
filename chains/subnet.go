// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// Subnet keeps track of the currently bootstrapping chains in a subnet. If no
// chains in the subnet are currently bootstrapping, the subnet is considered
// bootstrapped.
type Subnet interface {
	common.Subnet

	afterBootstrapped() chan struct{}

	addChain(chainID ids.ID)
	removeChain(chainID ids.ID)
}

type subnet struct {
	lock          sync.RWMutex
	bootstrapping ids.Set

	once sync.Once
	// If not nil, called when this subnet becomes marked as bootstrapped for
	// the first time
	onBootstrapped   func()
	bootstrappedSema chan struct{}
}

func newSubnet(onBootstrapped func(), firstChainID ids.ID) Subnet {
	sb := &subnet{
		onBootstrapped:   onBootstrapped,
		bootstrappedSema: make(chan struct{}),
	}
	sb.addChain(firstChainID)
	return sb
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
	if s.bootstrapping.Len() > 0 {
		return
	}

	if s.onBootstrapped != nil {
		s.once.Do(func() {
			if s.onBootstrapped != nil {
				s.onBootstrapped()
			}
			close(s.bootstrappedSema)
		})
	}
}

func (s *subnet) afterBootstrapped() chan struct{} {
	return s.bootstrappedSema
}

func (s *subnet) addChain(chainID ids.ID) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.bootstrapping.Add(chainID)
}

func (s *subnet) removeChain(chainID ids.ID) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.bootstrapping.Remove(chainID)
}

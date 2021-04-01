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

	addChain(chainID ids.ID)
	removeChain(chainID ids.ID)
}

type subnet struct {
	lock sync.RWMutex
	sync.Once
	bootstrapping ids.Set
	// Called once when IsBootstrapped returns true for the first time
	onFinish func()
}

func (s *subnet) IsBootstrapped() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	done := s.bootstrapping.Len() == 0
	if done {
		s.Once.Do(s.onFinish)
	}
	return done
}

func (s *subnet) Bootstrapped(chainID ids.ID) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.bootstrapping.Remove(chainID)
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

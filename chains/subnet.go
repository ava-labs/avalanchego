// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"bytes"
	"encoding/json"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var _ Subnet = &subnet{}

// Subnet keeps track of the currently bootstrapping chains in a subnet. If no
// chains in the subnet are currently bootstrapping, the subnet is considered
// bootstrapped.
type Subnet interface {
	common.Subnet

	afterBootstrapped() chan struct{}

	addChain(chainID ids.ID)
	removeChain(chainID ids.ID)
}

type SubnetConfig struct {
	// ValidatorOnly indicates that this Subnet's Chains are available to only subnet validators.
	ValidatorOnly       bool                 `json:"validatorOnly"`
	ConsensusParameters avalanche.Parameters `json:"consensusParameters"`
}

type subnet struct {
	lock             sync.RWMutex
	bootstrapping    ids.Set
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

func (config *SubnetConfig) UnmarshalJSON(data []byte) error {
	// without a new type, unmarshal gets into a infinite loop
	type xconfig SubnetConfig
	sc := xconfig(*config)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields() // Force errors

	if err := dec.Decode(&sc); err != nil {
		return err
	}

	*config = SubnetConfig(sc)
	return nil
}

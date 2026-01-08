// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"errors"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var ErrNoPrimaryNetworkConfig = errors.New("no subnet config for primary network found")

// Subnets holds the currently running subnets on this node
type Subnets struct {
	nodeID  ids.NodeID
	configs map[ids.ID]subnets.Config

	lock    sync.RWMutex
	subnets map[ids.ID]subnets.Subnet
}

// GetOrCreate returns a subnet running on this node, or creates one if it was
// not running before. Returns the subnet and if the subnet was created.
func (s *Subnets) GetOrCreate(subnetID ids.ID) (subnets.Subnet, bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if subnet, ok := s.subnets[subnetID]; ok {
		return subnet, false
	}

	// Default to the primary network config if a subnet config was not
	// specified
	config, ok := s.configs[subnetID]
	if !ok {
		config = s.configs[constants.PrimaryNetworkID]
	}

	subnet := subnets.New(s.nodeID, config)
	s.subnets[subnetID] = subnet

	return subnet, true
}

// Bootstrapping returns the subnetIDs of any chains that are still
// bootstrapping.
func (s *Subnets) Bootstrapping() []ids.ID {
	s.lock.RLock()
	defer s.lock.RUnlock()

	subnetsBootstrapping := make([]ids.ID, 0, len(s.subnets))
	for subnetID, subnet := range s.subnets {
		if !subnet.IsBootstrapped() {
			subnetsBootstrapping = append(subnetsBootstrapping, subnetID)
		}
	}

	return subnetsBootstrapping
}

// NewSubnets returns an instance of Subnets
func NewSubnets(
	nodeID ids.NodeID,
	configs map[ids.ID]subnets.Config,
) (*Subnets, error) {
	if _, ok := configs[constants.PrimaryNetworkID]; !ok {
		return nil, ErrNoPrimaryNetworkConfig
	}

	s := &Subnets{
		nodeID:  nodeID,
		configs: configs,
		subnets: make(map[ids.ID]subnets.Subnet),
	}

	_, _ = s.GetOrCreate(constants.PrimaryNetworkID)
	return s, nil
}

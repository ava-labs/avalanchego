// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

	lock    sync.Mutex
	subnets map[ids.ID]subnets.Subnet
}

// Add a subnet that is being run on this node. Returns if the subnet was added
// or not.
func (s *Subnets) Add(subnetID ids.ID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, ok := s.subnets[subnetID]; ok {
		return false
	}

	// Default to the primary network config if a subnet config was not
	// specified
	config, ok := s.configs[subnetID]
	if !ok {
		config = s.configs[constants.PrimaryNetworkID]
	}

	s.subnets[subnetID] = subnets.New(s.nodeID, config)
	return true
}

// Get returns a subnet if it is being run on this node. Returns the subnet
// if it was present.
func (s *Subnets) Get(subnetID ids.ID) (subnets.Subnet, bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	subnet, ok := s.subnets[subnetID]
	return subnet, ok
}

// Bootstrapping returns the subnetIDs of any chains that are still
// bootstrapping.
func (s *Subnets) Bootstrapping() []ids.ID {
	s.lock.Lock()
	defer s.lock.Unlock()

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

	_ = s.Add(constants.PrimaryNetworkID)
	return s, nil
}

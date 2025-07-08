// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

type AppRequestNetwork interface {
	Network

	AddSubnet(subnetID ids.ID)
	RemoveSubnet(subnetID ids.ID)

	Subnets() set.Set[ids.ID]
}

type appRequestNetwork struct {
	Network

	subnets     set.Set[ids.ID]
	subnetsLock sync.RWMutex
}

func (a *appRequestNetwork) AddSubnet(subnetID ids.ID) {
	a.subnetsLock.Lock()
	defer a.subnetsLock.Unlock()
	a.subnets.Add(subnetID)
}

func (a *appRequestNetwork) RemoveSubnet(subnetID ids.ID) {
	a.subnetsLock.Lock()
	defer a.subnetsLock.Unlock()
	a.subnets.Remove(subnetID)
}

func (a *appRequestNetwork) Subnets() set.Set[ids.ID] {
	a.subnetsLock.RLock()
	defer a.subnetsLock.RUnlock()
	return a.subnets
}

func NewAppRequestNetwork(network Network) AppRequestNetwork {
	return &appRequestNetwork{
		Network: network,
	}
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"net"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
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

func NewAppRequestNetwork(
	config *Config,
	minCompatibleTime time.Time,
	msgCreator message.Creator,
	metricsRegisterer prometheus.Registerer,
	log logging.Logger,
	listener net.Listener,
	dialer dialer.Dialer,
	router router.ExternalHandler,
) (AppRequestNetwork, error) {
	config.AppRequestOnlyClient = true
	network, err := newNetwork(config, minCompatibleTime, msgCreator, metricsRegisterer, log, listener, dialer, router)
	if err != nil {
		log.Error("failed to create network", zap.Error(err))
		return nil, err
	}
	appRequestNetwork := &appRequestNetwork{
		Network: network,
	}
	network.peerConfig.Network = appRequestNetwork
	return appRequestNetwork, nil
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"sync"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
)

var _ router.Router = (*beaconManager)(nil)

type beaconManager struct {
	router.Router
	beacons                     validators.Manager
	requiredConns               int64
	numConns                    int64
	onSufficientlyConnected     chan struct{}
	onceOnSufficientlyConnected sync.Once
}

func (b *beaconManager) Connected(nodeID ids.NodeID, nodeVersion *version.Application, subnetID ids.ID) {
	_, isBeacon := b.beacons.GetValidator(constants.PrimaryNetworkID, nodeID)
	if isBeacon &&
		constants.PrimaryNetworkID == subnetID &&
		atomic.AddInt64(&b.numConns, 1) >= b.requiredConns {
		b.onceOnSufficientlyConnected.Do(func() {
			close(b.onSufficientlyConnected)
		})
	}
	b.Router.Connected(nodeID, nodeVersion, subnetID)
}

func (b *beaconManager) Disconnected(nodeID ids.NodeID) {
	if _, isBeacon := b.beacons.GetValidator(constants.PrimaryNetworkID, nodeID); isBeacon {
		atomic.AddInt64(&b.numConns, -1)
	}
	b.Router.Disconnected(nodeID)
}

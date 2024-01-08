// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/ips"
)

var TestNetwork Network = testNetwork{}

type testNetwork struct{}

func (testNetwork) Connected(ids.NodeID) {}

func (testNetwork) AllowConnection(ids.NodeID) bool {
	return true
}

func (testNetwork) Track([]*ips.ClaimedIPPort) error {
	return nil
}

func (testNetwork) Disconnected(ids.NodeID) {}

func (testNetwork) KnownPeers() ([]byte, []byte) {
	return bloom.EmptyFilter.Marshal(), nil
}

func (testNetwork) Peers(ids.NodeID, *bloom.ReadFilter, []byte) []*ips.ClaimedIPPort {
	return nil
}

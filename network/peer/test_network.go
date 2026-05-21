// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/utils/bloom"
	"github.com/MetalBlockchain/metalgo/utils/ips"
	"github.com/MetalBlockchain/metalgo/utils/set"
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

func (testNetwork) Peers(
	ids.NodeID,
	set.Set[ids.ID],
	bool,
	*bloom.ReadFilter,
	[]byte,
) []*ips.ClaimedIPPort {
	return nil
}

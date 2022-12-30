// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/ips"
)

var TestNetwork Network = testNetwork{}

type testNetwork struct{}

func (testNetwork) Connected(ids.NodeID) {}

func (testNetwork) AllowConnection(ids.NodeID) bool {
	return true
}

func (testNetwork) Track(ids.NodeID, []*ips.ClaimedIPPort) ([]*p2p.PeerAck, error) {
	return nil, nil
}

func (testNetwork) MarkTracked(ids.NodeID, []*p2p.PeerAck) error {
	return nil
}

func (testNetwork) Disconnected(ids.NodeID) {}

func (testNetwork) Peers(ids.NodeID) ([]ips.ClaimedIPPort, error) {
	return nil, nil
}

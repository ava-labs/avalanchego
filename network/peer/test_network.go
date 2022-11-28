// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/version"
)

var _ Network = (*testNetwork)(nil)

// testNetwork is a network definition for a TestPeer
type testNetwork struct {
	mc message.Creator

	networkID uint32
	ip        ips.IPPort
	version   *version.Application
	signer    crypto.Signer
	subnets   ids.Set

	uptime uint8
}

// NewTestNetwork creates and returns a new TestNetwork
func NewTestNetwork(
	mc message.Creator,
	networkID uint32,
	ipPort ips.IPPort,
	version *version.Application,
	signer crypto.Signer,
	subnets ids.Set,
	uptime uint8,
) Network {
	return &testNetwork{
		mc:        mc,
		networkID: networkID,
		ip:        ipPort,
		version:   version,
		signer:    signer,
		subnets:   subnets,
		uptime:    uptime,
	}
}

func (*testNetwork) Connected(ids.NodeID) {}

func (*testNetwork) AllowConnection(ids.NodeID) bool {
	return true
}

func (*testNetwork) Track(ips.ClaimedIPPort) bool {
	return true
}

func (*testNetwork) Disconnected(ids.NodeID) {}

func (n *testNetwork) Version() (message.OutboundMessage, error) {
	now := uint64(time.Now().Unix())
	unsignedIP := UnsignedIP{
		IP:        n.ip,
		Timestamp: now,
	}
	signedIP, err := unsignedIP.Sign(n.signer)
	if err != nil {
		return nil, err
	}
	return n.mc.Version(
		n.networkID,
		now,
		n.ip,
		n.version.String(),
		now,
		signedIP.Signature,
		n.subnets.List(),
	)
}

func (n *testNetwork) Peers() (message.OutboundMessage, error) {
	return n.mc.PeerList(nil, true)
}

func (n *testNetwork) Pong(ids.NodeID) (message.OutboundMessage, error) {
	return n.mc.Pong(n.uptime)
}

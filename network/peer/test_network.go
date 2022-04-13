// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"time"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/message"
	"github.com/chain4travel/caminogo/utils"
	"github.com/chain4travel/caminogo/version"
)

var _ Network = &testNetwork{}

// testNetwork is a network definition for a TestPeer
type testNetwork struct {
	mc message.Creator

	networkID uint32
	ip        utils.IPDesc
	version   version.Application
	signer    crypto.Signer
	subnets   ids.Set

	uptime uint8
}

// NewTestNetwork creates and returns a new TestNetwork
func NewTestNetwork(
	mc message.Creator,
	networkID uint32,
	ipDesc utils.IPDesc,
	version version.Application,
	signer crypto.Signer,
	subnets ids.Set,
	uptime uint8,
) Network {
	return &testNetwork{
		mc:        mc,
		networkID: networkID,
		ip:        ipDesc,
		version:   version,
		signer:    signer,
		subnets:   subnets,
		uptime:    uptime,
	}
}

func (n *testNetwork) Connected(ids.ShortID) {}

func (n *testNetwork) AllowConnection(ids.ShortID) bool { return true }

func (n *testNetwork) Track(utils.IPCertDesc) {}

func (n *testNetwork) Disconnected(ids.ShortID) {}

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

func (n *testNetwork) Pong(ids.ShortID) (message.OutboundMessage, error) {
	return n.mc.Pong(n.uptime)
}

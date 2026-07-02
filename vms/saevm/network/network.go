// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ validators.Connector = (*Network)(nil)
	_ common.AppHandler    = (*Network)(nil)
)

type Network struct {
	*p2p.Network
	ValidatorPeers *p2p.Validators
	Peers          *p2p.Peers
	PeerTracker    *p2p.PeerTracker
}

// New creates the P2P network with a registered validator set.
func New(
	snowCtx *snow.Context,
	sender common.AppSender,
) (*Network, error) {
	reg, err := metrics.MakeAndRegister(snowCtx.Metrics, "network")
	if err != nil {
		return nil, fmt.Errorf("registering metrics: %w", err)
	}
	peers := &p2p.Peers{}
	const maxValidatorSetStaleness = time.Minute
	validatorPeers := p2p.NewValidators(
		snowCtx.Log,
		snowCtx.SubnetID,
		snowCtx.ValidatorState,
		maxValidatorSetStaleness,
	)
	peerTracker, err := p2p.NewPeerTracker(
		snowCtx.Log,
		"peer_tracker",
		reg,
		set.Of(snowCtx.NodeID),
		nil,
	)
	network, err := p2p.NewNetwork(
		snowCtx.Log,
		sender,
		reg,
		"p2p",
		peers,
		validatorPeers,
		&connectablePeerTracker{peerTracker},
	)
	if err != nil {
		return nil, err
	}
	return &Network{
		Network:        network,
		Peers:          peers,
		ValidatorPeers: validatorPeers,
		PeerTracker:    peerTracker,
	}, nil
}

var _ p2p.ConnectionHandler = (*connectablePeerTracker)(nil)

type connectablePeerTracker struct {
	*p2p.PeerTracker
}

func (c *connectablePeerTracker) Connected(nodeID ids.NodeID) {
	c.PeerTracker.Connected(nodeID, nil)
}

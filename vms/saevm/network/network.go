// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package network provides the P2P network for the SAE VM.
package network

import (
	"fmt"
	"time"

	"github.com/ava-labs/libevm/libevm/options"

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

// Config sets optional parameters for the P2P network.
type config struct {
	// stateSyncIDs provides an exclusive list of nodes that will be connected
	// through the [p2p.PeerTracker] on the [Network].
	stateSyncIDs set.Set[ids.NodeID]
}

// An Option provides overrides to default network behavior
type Option = options.Option[config]

func WithStateSyncIDs(ids set.Set[ids.NodeID]) Option {
	return options.Func[config](func(c *config) {
		c.stateSyncIDs = ids
	})
}

// Network contains the [p2p.Network] and all coupled state for use by the SAE
// VM. It should only be constructed with [New].
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
	opts ...Option,
) (*Network, error) {
	config := options.As(opts...)

	reg, err := metrics.MakeAndRegister(snowCtx.Metrics, "p2p")
	if err != nil {
		return nil, fmt.Errorf("registering metrics: %w", err)
	}
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
	if err != nil {
		return nil, fmt.Errorf("creating peer tracker: %w", err)
	}
	for id := range config.stateSyncIDs {
		peerTracker.Connected(id, nil)
	}

	peers := &p2p.Peers{}
	connectionHandlers := []p2p.ConnectionHandler{peers, validatorPeers}
	if len(config.stateSyncIDs) == 0 {
		connectionHandlers = append(
			connectionHandlers,
			peerTracker,
		)
	}

	network, err := p2p.NewNetwork(
		snowCtx.Log,
		sender,
		reg,
		"p2p",
		connectionHandlers...,
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

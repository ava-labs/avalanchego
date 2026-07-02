// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package network provides the P2P network for the SAE VM.
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

// Config sets optional parameters for the P2P network.
type Config struct {
	// TrackedIDs provides an exclusive list of nodes that will be connected
	// through all [p2p.PeerTracker] on the [Network].
	TrackedIDs []ids.NodeID
}

// Network contains the [p2p.Network] and all coupled state for use by the SAE
// VM. It should only be constructed with [New].
type Network struct {
	*p2p.Network
	ValidatorPeers       *p2p.Validators
	Peers                *p2p.Peers
	TrieDependentTracker *p2p.PeerTracker
	PeerTracker          *p2p.PeerTracker
}

// New creates the P2P network with a registered validator set.
func New(
	config Config,
	snowCtx *snow.Context,
	sender common.AppSender,
) (*Network, error) {
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

	triePeerTracker, err := p2p.NewPeerTracker(
		snowCtx.Log,
		"trie_peer_tracker",
		reg,
		set.Of(snowCtx.NodeID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating trie peer tracker: %w", err)
	}
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
	for _, id := range config.TrackedIDs {
		for _, pt := range []*p2p.PeerTracker{peerTracker, triePeerTracker} {
			pt.Connected(id, nil)
		}
	}

	peers := &p2p.Peers{}
	connectionHandlers := []p2p.ConnectionHandler{peers, validatorPeers}
	if len(config.TrackedIDs) == 0 {
		connectionHandlers = append(
			connectionHandlers,
			&connectablePeerTracker{peerTracker},
			&connectablePeerTracker{triePeerTracker},
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
		Network:              network,
		Peers:                peers,
		ValidatorPeers:       validatorPeers,
		TrieDependentTracker: triePeerTracker,
		PeerTracker:          peerTracker,
	}, nil
}

var _ p2p.ConnectionHandler = (*connectablePeerTracker)(nil)

type connectablePeerTracker struct {
	*p2p.PeerTracker
}

func (c *connectablePeerTracker) Connected(nodeID ids.NodeID) {
	c.PeerTracker.Connected(nodeID, nil)
}

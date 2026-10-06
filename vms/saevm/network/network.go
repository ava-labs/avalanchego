// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package network provides the P2P network for the SAE VM.
package network

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ava-labs/libevm/libevm/options"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	_ validators.Connector = (*Network)(nil)
	_ common.AppHandler    = (*Network)(nil)
)

// config sets optional parameters for the P2P network.
type config struct {
	// trackedPeers provides an exclusive list of nodes that will be connected
	// through every [p2p.PeerTracker] on the [Network].
	trackedPeers set.Set[ids.NodeID]
}

// An Option provides overrides to default network behavior.
type Option = options.Option[config]

// WithAllowedTrackedPeers restricts the peers available to every
// [Network.TrackingClient] to only those in the provided set.
func WithAllowedTrackedPeers(ids set.Set[ids.NodeID]) Option {
	return options.Func[config](func(c *config) {
		c.trackedPeers = ids
	})
}

// Network contains the [p2p.Network] and all coupled state for use by the SAE
// VM. It should only be constructed with [New].
type Network struct {
	*p2p.Network
	ValidatorPeers *p2p.Validators
	Peers          *p2p.Peers
	peerTrackers   map[uint64]*p2p.PeerTracker
}

// syncHandlerIDs each get their own [p2p.PeerTracker], because bandwidth
// scales with response size and so is only comparable within one protocol.
var syncHandlerIDs = []uint64{
	p2p.EVMLeafRequestHandlerID,
	p2p.EVMCodeRequestHandlerID,
	p2p.EVMBlockRequestHandlerID,
	p2p.EVMAtomicLeafRequestHandlerID,
}

const handlerIDLabel = "handlerID"

// TrackingClient returns a client for the sync protocol served under handlerID,
// scored by that protocol's own [p2p.PeerTracker]. handlerID MUST be a sync protocol.
func (n *Network) TrackingClient(handlerID uint64) *p2p.TrackingClient {
	return n.NewTrackingClient(handlerID, n.peerTrackers[handlerID])
}

// New creates the P2P network with a registered validator set.
func New(
	snowCtx *snow.Context,
	sender common.AppSender,
	opts ...Option,
) (*Network, error) {
	cfg := options.As(opts...)

	reg, err := metrics.MakeAndRegister(snowCtx.Metrics, "p2p")
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

	connectionHandlers := []p2p.ConnectionHandler{peers, validatorPeers}
	peerTrackers := make(map[uint64]*p2p.PeerTracker, len(syncHandlerIDs))
	for _, handlerID := range syncHandlerIDs {
		peerTracker, err := p2p.NewPeerTracker(
			snowCtx.Log,
			"peer_tracker",
			prometheus.WrapRegistererWith(
				prometheus.Labels{handlerIDLabel: strconv.FormatUint(handlerID, 10)},
				reg,
			),
			set.Of(snowCtx.NodeID),
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("creating peer tracker for handler %d: %w", handlerID, err)
		}
		peerTrackers[handlerID] = peerTracker
		connectionHandlers = append(connectionHandlers, withFilter(peerTracker, cfg.trackedPeers))
	}

	const namespace = "network"
	network, err := p2p.NewNetwork(
		snowCtx.Log,
		sender,
		reg,
		namespace,
		connectionHandlers...,
	)
	if err != nil {
		return nil, err
	}
	return &Network{
		Network:        network,
		Peers:          peers,
		ValidatorPeers: validatorPeers,
		peerTrackers:   peerTrackers,
	}, nil
}

// withFilter wraps a [p2p.ConnectionHandler] to only connect to nodes in the
// provided set, if the set is non-empty.
func withFilter(handler p2p.ConnectionHandler, onlyInclude set.Set[ids.NodeID]) p2p.ConnectionHandler {
	if len(onlyInclude) == 0 {
		return handler
	}
	return &filteredConnections{
		ConnectionHandler: handler,
		onlyInclude:       onlyInclude,
	}
}

type filteredConnections struct {
	p2p.ConnectionHandler
	onlyInclude set.Set[ids.NodeID]
}

func (f *filteredConnections) Connected(id ids.NodeID, ver *version.Application) {
	if f.onlyInclude.Contains(id) {
		f.ConnectionHandler.Connected(id, ver)
	}
}

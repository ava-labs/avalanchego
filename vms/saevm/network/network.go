// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package network provides the P2P network for the SAE VM.
package network

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/libevm/options"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/peertracker"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errUnknownSyncProtocol = errors.New("unknown sync protocol")

	_ validators.Connector = (*Network)(nil)
	_ common.AppHandler    = (*Network)(nil)
)

// config sets optional parameters for the P2P network.
type config struct {
	// trackedPeers provides an exclusive list of nodes that will be connected
	// through every [peertracker.PeerTracker] on the [Network].
	trackedPeers set.Set[ids.NodeID]
}

// An Option provides overrides to default network behavior.
type Option = options.Option[config]

// WithAllowedTrackedPeers restricts the peers available to every
// [Network.SyncClient] to only those in the provided set.
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
	peerTrackers   map[uint64]*peertracker.PeerTracker
}

// syncHandlerIDs each get their own [peertracker.PeerTracker], because bandwidth
// scales with response size and so is only comparable within one protocol.
var syncHandlerIDs = map[string]uint64{
	"leaf":        p2p.EVMLeafRequestHandlerID,
	"code":        p2p.EVMCodeRequestHandlerID,
	"block":       p2p.EVMBlockRequestHandlerID,
	"cchain_leaf": p2p.EVMAtomicLeafRequestHandlerID,
}

// SyncClient returns a client for the sync protocol served under handlerID,
// scored by that protocol's own [peertracker.PeerTracker].
func (n *Network) SyncClient(handlerID uint64) (*peertracker.Client, error) {
	peerTracker, ok := n.peerTrackers[handlerID]
	if !ok {
		return nil, fmt.Errorf("%w: handler %d", errUnknownSyncProtocol, handlerID)
	}
	return peertracker.NewClient(n.Network, handlerID, peerTracker), nil
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
	peerTrackers := make(map[uint64]*peertracker.PeerTracker, len(syncHandlerIDs))
	for name, handlerID := range syncHandlerIDs {
		peerTracker, err := peertracker.New(
			snowCtx.Log,
			"peer_tracker_"+name,
			reg,
			set.Of(snowCtx.NodeID),
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("creating %s peer tracker: %w", name, err)
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

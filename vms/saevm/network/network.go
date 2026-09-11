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
	"github.com/ava-labs/avalanchego/version"
)

var (
	_ validators.Connector = (*Network)(nil)
	_ common.AppHandler    = (*Network)(nil)
)

// config sets optional parameters for the P2P network.
type config struct {
	// trackedPeers provides an exclusive list of nodes that will be connected
	// through the [p2p.PeerTracker] on the [Network].
	trackedPeers set.Set[ids.NodeID]
}

// An Option provides overrides to default network behavior.
type Option = options.Option[config]

// WithAllowedTrackedPeers restricts the peers available in the
// [Network.PeerTracker] to only those in the provided set.
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
	// PeerTracker ranks peers on their responses to scheme-independent
	// requests (blocks, code, cross-chain state).
	PeerTracker *p2p.PeerTracker
	// StateTriePeerTracker ranks peers solely on their responses to state trie
	// requests, which are specific to this node's trie scheme (HashDB leaves or
	// Firewood proofs). Keeping it separate from [Network.PeerTracker] ensures
	// a peer that serves scheme-independent requests well is not preferred for
	// state trie requests it may be unable to serve.
	StateTriePeerTracker *p2p.PeerTracker
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

	newTracker := func(namespace string) (*p2p.PeerTracker, error) {
		pt, err := p2p.NewPeerTracker(
			snowCtx.Log,
			namespace,
			reg,
			set.Of(snowCtx.NodeID),
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("creating %s: %w", namespace, err)
		}
		return pt, nil
	}
	peerTracker, err := newTracker("peer_tracker")
	if err != nil {
		return nil, err
	}
	stateTriePeerTracker, err := newTracker("state_trie_peer_tracker")
	if err != nil {
		return nil, err
	}

	const namespace = "network"
	network, err := p2p.NewNetwork(
		snowCtx.Log,
		sender,
		reg,
		namespace,
		peers,
		validatorPeers,
		withFilter(peerTracker, cfg.trackedPeers),
		withFilter(stateTriePeerTracker, cfg.trackedPeers),
	)
	if err != nil {
		return nil, err
	}
	return &Network{
		Network:              network,
		Peers:                peers,
		ValidatorPeers:       validatorPeers,
		PeerTracker:          peerTracker,
		StateTriePeerTracker: stateTriePeerTracker,
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

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peertrackertest

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/network/p2p/peertracker"
)

// NewSelfClient returns a [peertracker.Client] that routes to nodeID's handler,
// scoring against a tracker the test does not observe.
func NewSelfClient(
	t *testing.T,
	ctx context.Context,
	nodeID ids.NodeID,
	handler p2p.Handler,
) *peertracker.Client {
	return NewSelfClientWithTracker(t, ctx, nodeID, handler, New(t))
}

// NewSelfClientWithTracker returns a [peertracker.Client] routing to nodeID's
// handler, scoring against tracker, with nodeID already connected to it.
func NewSelfClientWithTracker(
	t *testing.T,
	ctx context.Context,
	nodeID ids.NodeID,
	handler p2p.Handler,
	tracker *peertracker.PeerTracker,
) *peertracker.Client {
	network := p2ptest.NewSelfNetwork(t, ctx, nodeID, handler, tracker)
	return peertracker.NewClient(network, 0, tracker)
}

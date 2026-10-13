// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trackingtest

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/network/p2p/tracking"
)

// NewSelfClient returns a [tracking.Client] that routes to nodeID's handler,
// scoring against a tracker the test does not observe.
func NewSelfClient(
	t *testing.T,
	ctx context.Context,
	nodeID ids.NodeID,
	handler p2p.Handler,
) *tracking.Client {
	return NewSelfClientWithTracker(t, ctx, nodeID, handler, p2ptest.NewTracker(t))
}

// NewSelfClientWithTracker returns a [tracking.Client] routing to nodeID's
// handler, scoring against tracker, with nodeID already connected to it.
func NewSelfClientWithTracker(
	t *testing.T,
	ctx context.Context,
	nodeID ids.NodeID,
	handler p2p.Handler,
	tracker *p2p.PeerTracker,
) *tracking.Client {
	network := p2ptest.NewSelfNetwork(t, ctx, nodeID, handler, tracker)
	return tracking.NewClient(network, 0, tracker)
}

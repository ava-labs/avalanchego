// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

// NewSelfNetwork returns a single-node [p2p.Network] that loops every request
// back to its own handlers, and a [p2p.PeerTracker] that selects that node.
// Register handlers on the returned network and build a client against it to
// drive a full request/response round trip in-process.
func NewSelfNetwork(t *testing.T, ctx context.Context, nodeID ids.NodeID) (*p2p.Network, *p2p.PeerTracker) {
	t.Helper()

	sender := &enginetest.Sender{}
	net, err := p2p.NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(t, err)

	// Loop each send back into the same network asynchronously to avoid
	// deadlocking when the response is delivered on the sending goroutine.
	sender.SendAppRequestF = func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
		for range nodeIDs {
			go func() { _ = net.AppRequest(ctx, nodeID, requestID, time.Time{}, requestBytes) }()
		}
		return nil
	}
	sender.SendAppResponseF = func(ctx context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
		go func() { _ = net.AppResponse(ctx, nodeID, requestID, responseBytes) }()
		return nil
	}
	sender.SendAppErrorF = func(ctx context.Context, _ ids.NodeID, requestID uint32, code int32, message string) error {
		go func() {
			_ = net.AppRequestFailed(ctx, nodeID, requestID, &common.AppError{Code: code, Message: message})
		}()
		return nil
	}

	require.NoError(t, net.Connected(ctx, nodeID, nil))

	tracker, err := p2p.NewPeerTracker(logging.NoLog{}, "synctest_peer_tracker", prometheus.NewRegistry(), nil, nil)
	require.NoError(t, err)
	tracker.Connected(nodeID, &version.Application{Major: 99})

	return net, tracker
}

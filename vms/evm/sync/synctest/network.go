// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"
)

// EchoHandler returns a [p2p.Handler] that replies with b for every
// AppRequest.
func EchoHandler(b []byte) p2p.Handler {
	return p2p.TestHandler{
		AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
			return b, nil
		},
	}
}

// NewPeerTracker returns a [p2p.PeerTracker] with peers pre-connected
// at a set version, above any minVersion gate.
func NewPeerTracker(t *testing.T, peers ...ids.NodeID) *p2p.PeerTracker {
	t.Helper()
	tracker, err := p2p.NewPeerTracker(logging.NoLog{}, "test", prometheus.NewRegistry(), nil, nil)
	require.NoError(t, err)
	for _, nodeID := range peers {
		tracker.Connected(nodeID, &version.Application{Major: 99})
	}
	return tracker
}

// NewDispatcher returns a [network.Dispatcher] backed by a
// [p2ptest.NewSelfClient]. Behaves identically to one built via
// [network.NewDispatcher] in production.
func NewDispatcher[Req, Resp proto.Message](
	t *testing.T,
	ctx context.Context,
	nodeID ids.NodeID,
	h p2p.Handler,
	peers *p2p.PeerTracker,
) *network.Dispatcher[Req, Resp] {
	t.Helper()
	return network.NewDispatcher[Req, Resp](
		p2ptest.NewSelfClient(t, ctx, nodeID, h),
		peers,
	)
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

func newTestPeerTracker(t *testing.T, peers ...ids.NodeID) *p2p.PeerTracker {
	t.Helper()
	tracker, err := p2p.NewPeerTracker(
		logging.NoLog{},
		"test_peer_tracker",
		prometheus.NewRegistry(),
		nil,
		nil,
	)
	require.NoError(t, err)
	for _, nodeID := range peers {
		tracker.Connected(nodeID, &version.Application{Major: 99})
	}
	return tracker
}

func echoHandler(b []byte) p2p.Handler {
	return p2p.TestHandler{
		AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
			return b, nil
		},
	}
}

// LeafsClient is the test vehicle for Dispatcher behavior. The dispatch
// path is shared by every per-RPC client.
func newTestLeafsClient(t *testing.T, ctx context.Context, nodeID ids.NodeID, handler p2p.Handler, peers *p2p.PeerTracker) *LeafsClient {
	t.Helper()
	return &LeafsClient{
		client: p2ptest.NewSelfClient(t, ctx, nodeID, handler),
		peers:  peers,
	}
}

func TestDispatcher_SendTo(t *testing.T) {
	ctx := t.Context()
	nodeID := ids.GenerateTestNodeID()

	want := &syncpb.LeafsResponse{Keys: [][]byte{{1, 2, 3}}}
	wantBytes, err := proto.Marshal(want)
	require.NoError(t, err)

	c := newTestLeafsClient(t, ctx, nodeID, echoHandler(wantBytes), newTestPeerTracker(t, nodeID))

	got := &syncpb.LeafsResponse{}
	require.NoError(t, c.SendTo(ctx, nodeID, &syncpb.GetLeafsRequest{}, got))
	require.Empty(t, cmp.Diff(want, got, protocmp.Transform()))
}

func TestDispatcher_FailurePaths(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	tests := []struct {
		name    string
		peers   []ids.NodeID
		handler p2p.Handler
		wantErr error
	}{
		{
			name:    "no peer to send to",
			handler: p2p.NoOpHandler{},
			wantErr: errNoPeers,
		},
		{
			name:  "handler returns AppError",
			peers: []ids.NodeID{nodeID},
			handler: p2p.TestHandler{
				AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
					return nil, &common.AppError{Code: 42, Message: "boom"}
				},
			},
			wantErr: errHandlerFailed,
		},
		{
			name:    "response bytes are not valid proto",
			peers:   []ids.NodeID{nodeID},
			handler: echoHandler([]byte{0xff, 0xff, 0xff}),
			wantErr: errUnmarshalResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			c := newTestLeafsClient(t, ctx, nodeID, tt.handler, newTestPeerTracker(t, tt.peers...))
			_, err := c.Send(ctx, &syncpb.GetLeafsRequest{}, &syncpb.LeafsResponse{})
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestDispatcher_ContextCancelled(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	released := make(chan struct{})
	defer close(released)
	handler := p2p.TestHandler{
		AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
			<-released
			return nil, nil
		},
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	c := newTestLeafsClient(t, ctx, nodeID, handler, newTestPeerTracker(t, nodeID))
	_, err := c.Send(ctx, &syncpb.GetLeafsRequest{}, &syncpb.LeafsResponse{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestTrackerSampler(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	require.Equal(t,
		[]ids.NodeID{nodeID},
		trackerSampler{peers: newTestPeerTracker(t, nodeID)}.Sample(t.Context(), 1),
	)
	require.Empty(t, trackerSampler{peers: newTestPeerTracker(t, nodeID)}.Sample(t.Context(), 0))
	require.Empty(t, trackerSampler{peers: newTestPeerTracker(t)}.Sample(t.Context(), 1))
}

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

// echoHandler returns the supplied bytes for any AppRequest.
func echoHandler(b []byte) p2p.Handler {
	return p2p.TestHandler{
		AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
			return b, nil
		},
	}
}

func TestClient_RoundTrip(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	tests := []struct {
		name     string
		request  proto.Message
		wantResp proto.Message
	}{
		{
			name: "GetLeafs",
			request: &syncpb.GetLeafsRequest{
				RootHash: []byte{0xab},
				KeyLimit: 1024,
				NodeType: syncpb.NodeType_NODE_TYPE_STATE_TRIE,
			},
			wantResp: &syncpb.LeafsResponse{
				Keys:      [][]byte{{1, 2, 3}},
				Values:    [][]byte{{4, 5, 6}},
				ProofVals: [][]byte{{7, 8}},
			},
		},
		{
			name:     "GetCode",
			request:  &syncpb.GetCodeRequest{Hashes: [][]byte{{1}, {2}}},
			wantResp: &syncpb.CodeResponse{Data: [][]byte{{0xde, 0xad}, {0xbe, 0xef}}},
		},
		{
			name:     "GetBlocks",
			request:  &syncpb.GetBlockRequest{Hash: []byte{1}, Height: 100, Parents: 5},
			wantResp: &syncpb.BlockResponse{Blocks: [][]byte{{0x01}, {0x02}, {0x03}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			wantBytes, err := proto.Marshal(tt.wantResp)
			require.NoError(t, err)

			c := &Client{peers: newTestPeerTracker(t, nodeID)}
			p2pClient := p2ptest.NewSelfClient(t, ctx, nodeID, echoHandler(wantBytes))

			var got proto.Message
			switch req := tt.request.(type) {
			case *syncpb.GetLeafsRequest:
				c.leafs = p2pClient
				got, err = c.GetLeafs(ctx, req)
			case *syncpb.GetCodeRequest:
				c.code = p2pClient
				got, err = c.GetCode(ctx, req)
			case *syncpb.GetBlockRequest:
				c.blocks = p2pClient
				got, err = c.GetBlocks(ctx, req)
			default:
				t.Fatalf("unhandled request type %T", req)
			}

			require.NoError(t, err)
			require.Empty(t, cmp.Diff(tt.wantResp, got, protocmp.Transform()))
		})
	}
}

func TestClient_FailurePaths(t *testing.T) {
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
			c := &Client{
				leafs: p2ptest.NewSelfClient(t, ctx, nodeID, tt.handler),
				peers: newTestPeerTracker(t, tt.peers...),
			}
			_, err := c.GetLeafs(ctx, &syncpb.GetLeafsRequest{})
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestClient_ContextCancelled(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	// Handler blocks until the test ends, keeping the response channel empty
	// so our select fires deterministically on ctx.Done.
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

	c := &Client{
		leafs: p2ptest.NewSelfClient(t, ctx, nodeID, handler),
		peers: newTestPeerTracker(t, nodeID),
	}

	_, err := c.GetLeafs(ctx, &syncpb.GetLeafsRequest{})
	require.ErrorIs(t, err, context.Canceled)
}

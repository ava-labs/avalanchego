// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/version"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestDispatcher_SendBytes(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	want := &syncpb.GetLeafResponse{Keys: [][]byte{{1, 2, 3}}}
	wantBytes, err := proto.Marshal(want)
	require.NoError(t, err, "proto.Marshal(want)")

	reqBytes, err := proto.Marshal(&syncpb.GetLeafRequest{})
	require.NoError(t, err, "proto.Marshal(req)")

	tests := []struct {
		name    string
		handler p2p.Handler
		cancel  bool
		want    *syncpb.GetLeafResponse
		wantErr error
	}{
		{
			name:    "round trip",
			handler: echoHandler(wantBytes),
			want:    want,
		},
		{
			name:    "handler returns AppError",
			handler: errorHandler(),
			wantErr: errHandlerFailed,
		},
		{
			name:    "response bytes are not valid proto",
			handler: echoHandler([]byte{0xff, 0xff, 0xff}),
			wantErr: errUnmarshalResponse,
		},
		{
			// Pre-send cancel returns at the ctx.Err() guard, before the handler.
			name:    "context cancelled before send",
			handler: p2p.NoOpHandler{},
			cancel:  true,
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			tracker := newTestTracker(t, nodeID)
			c := newTestDispatcher[*syncpb.GetLeafRequest, syncpb.GetLeafResponse, *syncpb.GetLeafResponse, *syncpb.GetLeafResponse](
				t, ctx, nodeID, tt.handler, tracker,
			)

			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			got, err := c.sendBytes(ctx, nodeID, reqBytes, acceptLeaf)
			require.ErrorIsf(t, err, tt.wantErr, "%T.sendBytes()", c)
			if tt.wantErr != nil {
				return
			}

			assert.Empty(t, cmp.Diff(tt.want, got, protocmp.Transform()), "cmp.Diff(want, got)")
		})
	}
}

// Mid-flight cancel returns context.Canceled and leaves the peer's score
// alone, since it is answering. The handler cancels the context to ensure it.
func TestDispatcher_CancelInFlight(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	release := make(chan struct{})
	defer close(release) // avoid leaking the handler goroutine
	handler := p2p.TestHandler{
		AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
			cancel()
			<-release
			return nil, nil
		},
	}

	tracker := newTestTracker(t, nodeID)
	seedResponsive(t, tracker, nodeID)
	c := newTestDispatcher[*syncpb.GetLeafRequest, syncpb.GetLeafResponse, *syncpb.GetLeafResponse, *syncpb.GetLeafResponse](
		t, t.Context(), nodeID, handler, tracker,
	)

	reqBytes, err := proto.Marshal(&syncpb.GetLeafRequest{})
	require.NoError(t, err, "proto.Marshal(req)")

	_, err = c.sendBytes(ctx, nodeID, reqBytes, acceptLeaf)
	require.ErrorIsf(t, err, context.Canceled, "%T.sendBytes()", c)
	assert.Truef(t, responsive(tracker, nodeID), "%T.ResponsivePeers()", tracker)
}

// Success scores the peer responsive, failure de-scores it. De-score rows
// seed responsive first so the drop to 0 is a real transition.
func TestDispatcher_PeerScoring(t *testing.T) {
	okBytes, err := proto.Marshal(&syncpb.GetLeafResponse{})
	require.NoError(t, err, "proto.Marshal()")

	reqBytes, err := proto.Marshal(&syncpb.GetLeafRequest{})
	require.NoError(t, err, "proto.Marshal(req)")

	tests := []struct {
		name           string
		seed           bool
		handler        p2p.Handler
		wantErr        error
		rejectResp     bool
		wantResponsive bool
	}{
		{
			name:           "success scores responsive",
			handler:        echoHandler(okBytes),
			wantResponsive: true,
		},
		{
			name:       "rejected response de-scores",
			seed:       true,
			handler:    echoHandler(okBytes),
			rejectResp: true,
			wantErr:    errRejected,
		},
		{
			name:    "handler error de-scores",
			seed:    true,
			handler: errorHandler(),
			wantErr: errHandlerFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			nodeID := ids.GenerateTestNodeID()
			tracker := newTestTracker(t, nodeID)
			if tt.seed {
				seedResponsive(t, tracker, nodeID)
			}
			c := newTestDispatcher[*syncpb.GetLeafRequest, syncpb.GetLeafResponse, *syncpb.GetLeafResponse, *syncpb.GetLeafResponse](
				t, ctx, nodeID, tt.handler, tracker,
			)

			verify := acceptLeaf
			if tt.rejectResp {
				verify = rejectLeaf
			}
			_, err := c.sendBytes(ctx, nodeID, reqBytes, verify)
			require.ErrorIsf(t, err, tt.wantErr, "%T.sendBytes()", c)

			assert.Equalf(t, tt.wantResponsive, responsive(tracker, nodeID), "%T.ResponsivePeers()", tracker)
		})
	}
}

func echoHandler(b []byte) p2p.Handler {
	h, _ := scriptedHandler(scriptResponse{
		bytes: b,
	})
	return h
}

func errorHandler() p2p.Handler {
	h, _ := scriptedHandler(scriptResponse{
		appErr: &common.AppError{
			Code:    42,
			Message: "boom",
		},
	})
	return h
}

type scriptResponse struct {
	bytes  []byte
	appErr *common.AppError
}

// scriptedHandler replies with each response in order, then repeats the last.
func scriptedHandler(responses ...scriptResponse) (p2p.Handler, *atomic.Int32) {
	var calls atomic.Int32
	h := p2p.TestHandler{
		AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
			i := int(calls.Add(1)) - 1
			if i >= len(responses) {
				i = len(responses) - 1
			}
			return responses[i].bytes, responses[i].appErr
		},
	}
	return h, &calls
}

// seedResponsive marks nodeID responsive so a later de-score is a real
// 1 -> 0 transition.
func seedResponsive(t *testing.T, tracker *p2p.PeerTracker, nodeID ids.NodeID) {
	t.Helper()
	tracker.RegisterRequest(nodeID)
	tracker.RegisterResponse(nodeID, 1)
	require.Truef(t, responsive(tracker, nodeID), "%T.ResponsivePeers()", tracker)
}

func newTestTracker(t *testing.T, peers ...ids.NodeID) *p2p.PeerTracker {
	t.Helper()
	tracker := p2ptest.NewTracker(t)
	for _, nodeID := range peers {
		tracker.Connected(nodeID, &version.Application{Major: 99})
	}
	return tracker
}

func newTestDispatcher[Req proto.Message, In any, Resp ProtoMessage[In], Out any](
	t *testing.T,
	ctx context.Context,
	nodeID ids.NodeID,
	h p2p.Handler,
	peers *p2p.PeerTracker,
) *Dispatcher[Req, In, Resp, Out] {
	t.Helper()
	return &Dispatcher[Req, In, Resp, Out]{
		log:    loggingtest.New(t, logging.Debug),
		client: p2ptest.NewSelfTrackingClientWithTracker(t, ctx, nodeID, h, peers),
		peers:  peers,
	}
}

func responsive(tracker *p2p.PeerTracker, nodeID ids.NodeID) bool {
	peers := tracker.ResponsivePeers()
	return peers.Contains(nodeID)
}

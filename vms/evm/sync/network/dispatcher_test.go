// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"
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

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestDispatcher_Send(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	want := &syncpb.GetLeafResponse{Keys: [][]byte{{1, 2, 3}}}
	wantBytes, err := proto.Marshal(want)
	require.NoError(t, err, "proto.Marshal(want)")

	tests := []struct {
		name       string
		disconnect bool
		handler    p2p.Handler
		cancel     bool
		want       *syncpb.GetLeafResponse
		wantErr    error
	}{
		{
			name:    "round_trip",
			handler: echoHandler(wantBytes),
			want:    want,
		},
		{
			name:       "no_peer_to_send_to",
			disconnect: true,
			handler:    p2p.NoOpHandler{},
			wantErr:    p2p.ErrNoPeers,
		},
		{
			name:    "handler_returns_app_error",
			handler: errorHandler(),
			wantErr: errHandlerFailed,
		},
		{
			name:    "response_bytes_are_not_valid_proto",
			handler: echoHandler([]byte{0xff, 0xff, 0xff}),
			wantErr: errUnmarshalResponse,
		},
		{
			// Pre-send cancel returns at the ctx.Err() guard, before the handler.
			name:    "context_cancelled_before_send",
			handler: p2p.NoOpHandler{},
			cancel:  true,
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			_, tracker := newTestTracker(t)
			c := newTestDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
				t, ctx, nodeID, tt.handler, tracker,
			)
			if tt.disconnect {
				tracker.Disconnected(nodeID)
			}

			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			got := &syncpb.GetLeafResponse{}
			err := c.Send(ctx, &syncpb.GetLeafRequest{}, got, accept)
			require.ErrorIsf(t, err, tt.wantErr, "%T.Send()", c)
			if tt.wantErr != nil {
				return
			}

			assert.Empty(t, cmp.Diff(tt.want, got, protocmp.Transform()), "cmp.Diff(want, got)")
		})
	}
}

// Mid-flight cancel (parked in Send's select) returns context.Canceled
// and de-scores the peer. The handler cancels its own context to ensure it.
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

	reg, tracker := newTestTracker(t)
	c := newTestDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
		t, t.Context(), nodeID, handler, tracker,
	)
	p2ptest.SeedResponsive(t, tracker, nodeID)

	err := c.Send(ctx, &syncpb.GetLeafRequest{}, &syncpb.GetLeafResponse{}, accept)
	require.ErrorIsf(t, err, context.Canceled, "%T.Send()", c)
	assert.Equal(t, 0.0, p2ptest.TrackerGauge(t, reg, "test_peer_tracker", "num_responsive_peers"), "responsivePeers()")
}

// Success scores the peer responsive, failure de-scores it. De-score rows
// seed responsive first so the drop to 0 is a real transition.
func TestDispatcher_PeerScoring(t *testing.T) {
	okBytes, err := proto.Marshal(&syncpb.GetLeafResponse{})
	require.NoError(t, err, "proto.Marshal()")

	tests := []struct {
		name       string
		seed       bool
		handler    p2p.Handler
		rejectResp bool
		wantErr    error
		wantPeers  float64
	}{
		{
			name:      "accepted_response_scores_responsive",
			handler:   echoHandler(okBytes),
			wantPeers: 1,
		},
		{
			name:       "rejected_response_de_scores",
			seed:       true,
			handler:    echoHandler(okBytes),
			rejectResp: true,
			wantErr:    errRejected,
			wantPeers:  0,
		},
		{
			name:      "handler_error_de_scores",
			seed:      true,
			handler:   errorHandler(),
			wantErr:   errHandlerFailed,
			wantPeers: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			nodeID := ids.GenerateTestNodeID()
			reg, tracker := newTestTracker(t)
			c := newTestDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
				t, ctx, nodeID, tt.handler, tracker,
			)
			if tt.seed {
				p2ptest.SeedResponsive(t, tracker, nodeID)
			}

			validate := accept
			if tt.rejectResp {
				validate = reject
			}
			err := c.Send(ctx, &syncpb.GetLeafRequest{}, &syncpb.GetLeafResponse{}, validate)
			require.ErrorIsf(t, err, tt.wantErr, "%T.Send()", c)

			assert.Equal(t, tt.wantPeers, p2ptest.TrackerGauge(t, reg, "test_peer_tracker", "num_responsive_peers"), "responsivePeers()")
		})
	}
}

var errRejected = errors.New("rejected by caller")

func accept(ids.NodeID, *syncpb.GetLeafResponse) error { return nil }

func reject(ids.NodeID, *syncpb.GetLeafResponse) error { return errRejected }

func echoHandler(b []byte) p2p.Handler {
	return p2p.TestHandler{
		AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
			return b, nil
		},
	}
}

func errorHandler() p2p.Handler {
	return p2p.TestHandler{
		AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
			return nil, &common.AppError{Code: 42, Message: "boom"}
		},
	}
}

// newTestTracker returns an empty tracker. The client mesh connects its peer.
func newTestTracker(t *testing.T) (*prometheus.Registry, *p2p.PeerTracker) {
	t.Helper()
	reg := prometheus.NewRegistry()
	tracker, err := p2p.NewPeerTracker(loggingtest.New(t, logging.Debug), "test_peer_tracker", reg, nil, nil)
	require.NoError(t, err, "p2p.NewPeerTracker()")
	return reg, tracker
}

func newTestDispatcher[Req, Resp proto.Message](
	t *testing.T,
	ctx context.Context,
	nodeID ids.NodeID,
	h p2p.Handler,
	peers *p2p.PeerTracker,
) *Dispatcher[Req, Resp] {
	t.Helper()
	return &Dispatcher[Req, Resp]{
		client: p2ptest.NewSelfTrackedClient(t, ctx, nodeID, h, peers),
	}
}

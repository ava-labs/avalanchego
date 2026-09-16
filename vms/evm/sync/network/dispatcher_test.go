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

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// De-score rows seed responsive first, so the drop to 0 is a real transition.
func TestDispatcher_Send(t *testing.T) {
	want := &syncpb.GetLeafResponse{Keys: [][]byte{{1, 2, 3}}}
	wantBytes, err := proto.Marshal(want)
	require.NoError(t, err, "proto.Marshal(want)")

	okBytes, err := proto.Marshal(&syncpb.GetLeafResponse{})
	require.NoError(t, err, "proto.Marshal()")

	tests := []struct {
		name       string
		handler    p2p.Handler
		seed       bool
		disconnect bool
		cancel     bool
		rejectResp bool
		want       *syncpb.GetLeafResponse
		wantErr    error
		wantPeers  float64
	}{
		{
			name:      "round_trip",
			handler:   echoHandler(wantBytes),
			want:      want,
			wantPeers: 1,
		},
		{
			name:       "no_peer_to_send_to",
			handler:    p2p.NoOpHandler{},
			disconnect: true,
			wantErr:    p2p.ErrNoPeers,
		},
		{
			name:      "handler_returns_app_error",
			handler:   errorHandler(),
			seed:      true,
			wantErr:   errHandlerFailed,
			wantPeers: 0,
		},
		{
			name:      "response_bytes_are_not_valid_proto",
			handler:   echoHandler([]byte{0xff, 0xff, 0xff}),
			seed:      true,
			wantErr:   errUnmarshalResponse,
			wantPeers: 0,
		},
		{
			// Pre-send cancel returns at the ctx.Err() guard, before the handler.
			name:    "context_cancelled_before_send",
			handler: p2p.NoOpHandler{},
			cancel:  true,
			wantErr: context.Canceled,
		},
		{
			name:       "response_rejected_by_caller",
			handler:    echoHandler(okBytes),
			seed:       true,
			rejectResp: true,
			wantErr:    errRejected,
			wantPeers:  0,
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
			if tt.disconnect {
				tracker.Disconnected(nodeID)
			}
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			validate := accept
			if tt.rejectResp {
				validate = reject
			}

			got := &syncpb.GetLeafResponse{}
			err := c.Send(ctx, &syncpb.GetLeafRequest{}, got, validate)
			require.ErrorIsf(t, err, tt.wantErr, "%T.Send()", c)
			assert.Equal(t, tt.wantPeers, p2ptest.TrackerGauge(t, reg, "test_peer_tracker", "num_responsive_peers"), "responsivePeers()")

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

// newTestTracker returns an empty tracker and the registry it publishes to.
func newTestTracker(t *testing.T) (*prometheus.Registry, *p2p.PeerTracker) {
	t.Helper()
	reg := prometheus.NewRegistry()
	return reg, p2ptest.NewTrackerWithRegistry(t, "test_peer_tracker", reg)
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
		client: p2ptest.NewSelfTrackingClient(t, ctx, nodeID, h, peers),
	}
}

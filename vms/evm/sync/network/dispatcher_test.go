// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
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
	"github.com/ava-labs/avalanchego/version"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestDispatcher_Send(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	want := &syncpb.GetLeafResponse{Keys: [][]byte{{1, 2, 3}}}
	wantBytes, err := proto.Marshal(want)
	require.NoError(t, err, "proto.Marshal(want)")

	tests := []struct {
		name    string
		peers   []ids.NodeID
		handler p2p.Handler
		cancel  bool
		want    *syncpb.GetLeafResponse
		wantErr error
	}{
		{
			name:    "round trip",
			peers:   []ids.NodeID{nodeID},
			handler: echoHandler(wantBytes),
			want:    want,
		},
		{
			name:    "no peer to send to",
			handler: p2p.NoOpHandler{},
			wantErr: errNoPeers,
		},
		{
			name:    "handler returns AppError",
			peers:   []ids.NodeID{nodeID},
			handler: errorHandler(),
			wantErr: errHandlerFailed,
		},
		{
			name:    "response bytes are not valid proto",
			peers:   []ids.NodeID{nodeID},
			handler: echoHandler([]byte{0xff, 0xff, 0xff}),
			wantErr: errUnmarshalResponse,
		},
		{
			// Pre-send cancel returns at SendTo's ctx.Err() guard before
			// the handler runs.
			name:    "context cancelled before send",
			peers:   []ids.NodeID{nodeID},
			handler: p2p.NoOpHandler{},
			cancel:  true,
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			_, tracker := newTestTracker(t, tt.peers...)
			c := newTestDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
				t, ctx, nodeID, tt.handler, tracker,
			)

			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			got := &syncpb.GetLeafResponse{}
			outcome, err := c.Send(ctx, &syncpb.GetLeafRequest{}, got)
			require.ErrorIsf(t, err, tt.wantErr, "%T.Send()", c)
			if tt.wantErr != nil {
				// Failures self-register, the caller gets no Outcome.
				require.Nilf(t, outcome, "%T.Send() outcome", c)
				return
			}

			require.NotNilf(t, outcome, "%T.Send() outcome", c)
			assert.Empty(t, cmp.Diff(tt.want, got, protocmp.Transform()), "cmp.Diff(want, got)")
		})
	}
}

// Cancel mid-flight (parked in SendTo's select) returns context.Canceled
// and de-scores the peer. Pre-send cancel is a row in TestDispatcher_Send.
func TestDispatcher_CancelInFlight(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	reqCtx, cancel := context.WithCancel(t.Context())

	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	handler := p2p.TestHandler{
		AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
			close(entered)
			<-release
			return nil, nil
		},
	}

	reg, tracker := newTestTracker(t, nodeID)
	seedResponsive(t, reg, tracker, nodeID)
	c := newTestDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
		t, t.Context(), nodeID, handler, tracker,
	)

	errCh := make(chan error, 1)
	go func() {
		_, err := c.SendTo(reqCtx, nodeID, &syncpb.GetLeafRequest{}, &syncpb.GetLeafResponse{})
		errCh <- err
	}()

	<-entered
	cancel()

	require.ErrorIsf(t, <-errCh, context.Canceled, "%T.SendTo()", c)
	assert.Equal(t, 0.0, responsivePeers(t, reg), "responsivePeers()")
}

// Success scores the peer responsive, failure de-scores it (via
// Outcome.Failure or SendTo's deferred RegisterFailure). De-score rows
// seed responsive first so the drop to 0 is a real transition.
func TestDispatcher_PeerScoring(t *testing.T) {
	okBytes, err := proto.Marshal(&syncpb.GetLeafResponse{})
	require.NoError(t, err, "proto.Marshal()")

	tests := []struct {
		name      string
		seed      bool
		handler   p2p.Handler
		wantErr   error
		score     func(*Outcome)
		wantPeers float64
	}{
		{
			// defer Failure() is the pessimistic default, Success() wins.
			name:      "success scores responsive",
			handler:   echoHandler(okBytes),
			score:     func(o *Outcome) { defer o.Failure(); o.Success() },
			wantPeers: 1,
		},
		{
			name:      "outcome failure de-scores",
			seed:      true,
			handler:   echoHandler(okBytes),
			score:     func(o *Outcome) { o.Failure() },
			wantPeers: 0,
		},
		{
			name:      "handler error de-scores",
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
			reg, tracker := newTestTracker(t, nodeID)
			if tt.seed {
				seedResponsive(t, reg, tracker, nodeID)
			}
			c := newTestDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
				t, ctx, nodeID, tt.handler, tracker,
			)

			outcome, err := c.SendTo(ctx, nodeID, &syncpb.GetLeafRequest{}, &syncpb.GetLeafResponse{})
			require.ErrorIsf(t, err, tt.wantErr, "%T.SendTo()", c)
			if tt.wantErr != nil {
				require.Nilf(t, outcome, "%T.SendTo() outcome", c)
			} else {
				require.NotNilf(t, outcome, "%T.SendTo() outcome", c)
				tt.score(outcome)
			}

			assert.Equal(t, tt.wantPeers, responsivePeers(t, reg), "responsivePeers()")
		})
	}
}

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

// seedResponsive marks nodeID responsive so a later de-score is a real
// 1 -> 0 transition.
func seedResponsive(t *testing.T, reg *prometheus.Registry, tracker *p2p.PeerTracker, nodeID ids.NodeID) {
	t.Helper()
	tracker.RegisterRequest(nodeID)
	tracker.RegisterResponse(nodeID, 1)
	require.Equal(t, 1.0, responsivePeers(t, reg), "responsivePeers()")
}

func newTestTracker(t *testing.T, peers ...ids.NodeID) (*prometheus.Registry, *p2p.PeerTracker) {
	t.Helper()
	reg := prometheus.NewRegistry()
	tracker, err := p2p.NewPeerTracker(logging.NoLog{}, "test_peer_tracker", reg, nil, nil)
	require.NoError(t, err, "p2p.NewPeerTracker()")
	for _, nodeID := range peers {
		tracker.Connected(nodeID, &version.Application{Major: 99})
	}
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
		client: p2ptest.NewSelfClient(t, ctx, nodeID, h),
		peers:  peers,
	}
}

// responsivePeers reads the num_responsive_peers gauge from reg.
func responsivePeers(t *testing.T, reg *prometheus.Registry) float64 {
	t.Helper()
	const name = "test_peer_tracker_num_responsive_peers"
	mfs, err := reg.Gather()
	require.NoError(t, err, "reg.Gather()")
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if m.Gauge != nil {
				return m.Gauge.GetValue()
			}
		}
	}
	t.Fatalf("metric %q not found", name)
	return 0
}

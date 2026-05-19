// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestDispatcher_SendTo(t *testing.T) {
	ctx := t.Context()
	nodeID := ids.GenerateTestNodeID()

	want := &syncpb.GetLeafResponse{Keys: [][]byte{{1, 2, 3}}}
	c := synctest.NewClient[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](t, ctx, nodeID, want)

	got := &syncpb.GetLeafResponse{}
	outcome, err := c.SendTo(ctx, nodeID, &syncpb.GetLeafRequest{}, got)
	require.NoError(t, err)
	require.NotNil(t, outcome)
	outcome.Success()
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
			wantErr: network.ErrNoPeers,
		},
		{
			name:  "handler returns AppError",
			peers: []ids.NodeID{nodeID},
			handler: p2p.TestHandler{
				AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
					return nil, &common.AppError{Code: 42, Message: "boom"}
				},
			},
			wantErr: network.ErrHandlerFailed,
		},
		{
			name:    "response bytes are not valid proto",
			peers:   []ids.NodeID{nodeID},
			handler: synctest.EchoHandler([]byte{0xff, 0xff, 0xff}),
			wantErr: network.ErrUnmarshalResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			c := synctest.NewDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
				t, ctx, nodeID, tt.handler, synctest.NewPeerTracker(t, tt.peers...),
			)
			outcome, err := c.Send(ctx, &syncpb.GetLeafRequest{}, &syncpb.GetLeafResponse{})
			require.ErrorIs(t, err, tt.wantErr)
			// Transport failures auto-register, caller gets no Outcome.
			require.Nil(t, outcome)
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

	c := synctest.NewDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
		t, ctx, nodeID, handler, synctest.NewPeerTracker(t, nodeID),
	)
	outcome, err := c.Send(ctx, &syncpb.GetLeafRequest{}, &syncpb.GetLeafResponse{})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, outcome)
}

// Exercises the canonical caller pattern: `defer outcome.Failure()` as
// a pessimistic default, then `outcome.Success()` on the happy path.
// Idempotency makes `Success` win. The deferred `Failure` that fires
// afterward is a no-op, and the peer stays in responsivePeers.
//
// NOTE: Builds its own [p2p.PeerTracker] (instead of using
// [synctest.NewPeerTracker]) so the test can read the responsive-peers
// gauge from the registry.
func TestOutcome_DeferFailureAndSuccess(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	ctx := t.Context()
	want := &syncpb.GetLeafResponse{Keys: [][]byte{{1}}}
	wantBytes, err := proto.Marshal(want)
	require.NoError(t, err)

	reg, tracker := newRegisteredTracker(t, nodeID)
	c := synctest.NewDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
		t, ctx, nodeID, synctest.EchoHandler(wantBytes), tracker,
	)

	outcome, err := c.Send(ctx, &syncpb.GetLeafRequest{}, &syncpb.GetLeafResponse{})
	require.NoError(t, err)
	require.NotNil(t, outcome)

	func() {
		defer outcome.Failure()
		outcome.Success()
	}()

	require.Equal(t, 1.0, gaugeValue(t, reg, "test_peer_tracker_num_responsive_peers"))
}

func TestOutcome_DeferFailureOnly(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	ctx := t.Context()
	want := &syncpb.GetLeafResponse{Keys: [][]byte{{1}}}
	wantBytes, err := proto.Marshal(want)
	require.NoError(t, err)

	reg, tracker := newRegisteredTracker(t, nodeID)
	c := synctest.NewDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](
		t, ctx, nodeID, synctest.EchoHandler(wantBytes), tracker,
	)

	outcome, err := c.Send(ctx, &syncpb.GetLeafRequest{}, &syncpb.GetLeafResponse{})
	require.NoError(t, err)
	require.NotNil(t, outcome)

	// Validation fails, no Success call. The deferred Failure fires
	// and the peer drops out of responsivePeers.
	func() {
		defer outcome.Failure()
		if err := errors.New("bad proof"); err != nil {
			return
		}
	}()

	require.Equal(t, 0.0, gaugeValue(t, reg, "test_peer_tracker_num_responsive_peers"))
}

func newRegisteredTracker(t *testing.T, nodeID ids.NodeID) (*prometheus.Registry, *p2p.PeerTracker) {
	t.Helper()
	reg := prometheus.NewRegistry()
	tracker, err := p2p.NewPeerTracker(logging.NoLog{}, "test_peer_tracker", reg, nil, nil)
	require.NoError(t, err)
	tracker.Connected(nodeID, &version.Application{Major: 99})
	return reg, tracker
}

// gaugeValue reads a single named gauge from `reg`.
func gaugeValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
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

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/libevm/libevm/options"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/version"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestNoPeersBackoff(t *testing.T) {
	p := *defaultRetryPolicy()

	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{
		{
			attempt: 0,
			want:    0,
		},
		{
			attempt: 1,
			want:    15 * time.Millisecond,
		},
		{
			attempt: 1000,
			want:    time.Second,
		},
	} {
		require.Equal(t, tc.want, p.noPeersBackoff(tc.attempt))
	}

	prev := time.Duration(0)
	for n := 0; n <= 30; n++ {
		d := p.noPeersBackoff(n)
		require.GreaterOrEqual(t, d, prev)
		require.LessOrEqual(t, d, p.noPeersMaxBackoff)
		prev = d
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want retryClass
	}{
		{
			name: "canceled",
			err:  context.Canceled,
			want: retryFatal,
		},
		{
			name: "deadline",
			err:  context.DeadlineExceeded,
			want: retryFatal,
		},
		{
			name: "wrapped_canceled",
			err:  fmt.Errorf("send: %w", context.Canceled),
			want: retryFatal,
		},
		{
			name: "marshal",
			err:  fmt.Errorf("%w: bad", errMarshalRequest),
			want: retryFatal,
		},
		{
			name: "no_peers",
			err:  errNoPeers,
			want: retryNoPeers,
		},
		{
			name: "send_request",
			err:  fmt.Errorf("%w: x", errSendRequest),
			want: retryPeerScoped,
		},
		{
			name: "handler_failed",
			err:  fmt.Errorf("%w: x", errHandlerFailed),
			want: retryPeerScoped,
		},
		{
			name: "unmarshal_response",
			err:  fmt.Errorf("%w: x", errUnmarshalResponse),
			want: retryPeerScoped,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classify(tt.err))
		})
	}
}

func TestSend_RetriesThenSucceeds(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	want := &syncpb.GetLeafResponse{Keys: [][]byte{{1, 2, 3}}}
	wantBytes, err := proto.Marshal(want)
	require.NoError(t, err)

	tests := []struct {
		name       string
		responses  []scriptResponse
		failVerify bool
	}{
		{
			name:      "handler_error",
			responses: []scriptResponse{{appErr: &common.AppError{Code: 1, Message: "boom"}}, {bytes: wantBytes}},
		},
		{
			name:      "unmarshal_error",
			responses: []scriptResponse{{bytes: []byte{0xff, 0xff}}, {bytes: wantBytes}},
		},
		{
			name:       "verify_failure",
			responses:  []scriptResponse{{bytes: wantBytes}, {bytes: wantBytes}},
			failVerify: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			handler, calls := scriptedHandler(tt.responses...)
			_, tracker := newTestTracker(t, nodeID)
			c := newRetryDispatcher(t, ctx, nodeID, handler, tracker)

			verify := acceptLeaf
			if tt.failVerify {
				rejected := false
				verify = func(*syncpb.GetLeafResponse, ids.NodeID) error {
					if !rejected {
						rejected = true
						return errors.New("invalid")
					}
					return nil
				}
			}

			got, err := c.Send(ctx, &syncpb.GetLeafRequest{}, verify)
			require.NoError(t, err)
			require.Empty(t, cmp.Diff(want, got, protocmp.Transform()))
			require.Len(t, got.GetKeys(), 1) // fresh response per attempt, no merge
			require.Equal(t, int32(2), calls.Load())
		})
	}
}

// Connecting mid-sleep, not before it, proves escalation: a working wait is
// asleep and misses it, noticing only at the next wake-up.
func TestSend_NoPeersBackoffEscalates(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	want := &syncpb.GetLeafResponse{Keys: [][]byte{{1, 2, 3}}}
	wantBytes, err := proto.Marshal(want)
	require.NoError(t, err)

	const (
		initial = 30 * time.Millisecond
		factor  = 4.0
		// A working escalation only notices this at its next ~600ms wake-up. A
		// flat or broken wait would notice within microseconds, well under minElapsed.
		connectAfter = 150 * time.Millisecond
		minElapsed   = 300 * time.Millisecond
	)
	ctx := t.Context()

	handler, _ := scriptedHandler(scriptResponse{bytes: wantBytes})
	_, tracker := newTestTracker(t)
	c := newTestDispatcher[*syncpb.GetLeafRequest, syncpb.GetLeafResponse, *syncpb.GetLeafResponse](t, ctx, nodeID, handler, tracker)
	c.policy = *options.ApplyTo(defaultRetryPolicy(),
		WithNoPeersInitialBackoff(initial),
		WithNoPeersFactor(factor),
		WithNoPeersMaxBackoff(time.Second),
	)

	start := time.Now()
	go func() {
		time.Sleep(connectAfter)
		tracker.Connected(nodeID, &version.Application{Major: 99})
	}()

	got, err := c.Send(ctx, &syncpb.GetLeafRequest{}, acceptLeaf)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.Empty(t, cmp.Diff(want, got, protocmp.Transform()))
	require.Greater(t, elapsed, minElapsed,
		"Send noticed the connected peer too soon, the no-peers wait is not escalating")
}

func TestSend_CtxCancelledBeforeStart(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	handler, calls := scriptedHandler(scriptResponse{bytes: []byte{}})
	_, tracker := newTestTracker(t, nodeID)
	c := newRetryDispatcher(t, ctx, nodeID, handler, tracker)

	got, err := c.Send(ctx, &syncpb.GetLeafRequest{}, acceptLeaf)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
	require.Zero(t, calls.Load())
}

// doRetry is exercised directly, not through Send, so the closure can end
// ctx exactly when it records the failure, with no real-time wait to race.
func TestDoRetry_CtxEndReportsFailure(t *testing.T) {
	errInvalid := errors.New("invalid")

	tests := []struct {
		name       string
		attemptErr error // nil picks the verify-rejects path
		wantLast   error
	}{
		{name: "verify_rejects", wantLast: errInvalid},
		{name: "no_peers", attemptErr: errNoPeers, wantLast: errNoPeers},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			attempt := func() (*syncpb.GetLeafResponse, ids.NodeID, *Outcome, error) {
				if tt.attemptErr != nil {
					cancel()
					var zero *syncpb.GetLeafResponse
					return zero, ids.EmptyNodeID, nil, tt.attemptErr
				}
				nodeID := ids.GenerateTestNodeID()
				_, tracker := newTestTracker(t, nodeID)
				return &syncpb.GetLeafResponse{}, nodeID, &Outcome{peers: tracker, nodeID: nodeID}, nil
			}
			verify := func(*syncpb.GetLeafResponse, ids.NodeID) error {
				cancel()
				return errInvalid
			}

			got, err := doRetry(ctx, loggingtest.New(t, logging.Debug), *defaultRetryPolicy(), verify, attempt)
			require.Nil(t, got)
			require.ErrorIs(t, err, context.Canceled)
			require.ErrorIs(t, err, tt.wantLast)
		})
	}
}

func acceptLeaf(*syncpb.GetLeafResponse, ids.NodeID) error { return nil }

type leafRetryDispatcher = Dispatcher[*syncpb.GetLeafRequest, syncpb.GetLeafResponse, *syncpb.GetLeafResponse]

func newRetryDispatcher(
	t *testing.T,
	ctx context.Context,
	nodeID ids.NodeID,
	h p2p.Handler,
	tracker *p2p.PeerTracker,
) *leafRetryDispatcher {
	t.Helper()
	c := newTestDispatcher[*syncpb.GetLeafRequest, syncpb.GetLeafResponse, *syncpb.GetLeafResponse](t, ctx, nodeID, h, tracker)
	c.policy = *options.ApplyTo(defaultRetryPolicy(),
		WithPeerFailureBackoff(time.Millisecond),
		WithNoPeersInitialBackoff(time.Millisecond),
		WithNoPeersMaxBackoff(5*time.Millisecond),
	)
	return c
}

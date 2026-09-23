// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ava-labs/libevm/libevm/options"
	"github.com/google/go-cmp/cmp"
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
			want:    15 * time.Millisecond, // initial * factor
		},
		{
			attempt: 1000,
			want:    time.Second, // max backoff
		},
	} {
		t.Run(fmt.Sprintf("attempt=%d", tc.attempt), func(t *testing.T) {
			require.Equal(t, tc.want, p.noPeersBackoff(tc.attempt))
		})
	}
}

func TestSend_RetriesThenSucceeds(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	want := &syncpb.GetLeafResponse{Keys: [][]byte{{1, 2, 3}}}
	wantBytes, err := proto.Marshal(want)
	require.NoError(t, err)

	tests := []struct {
		name            string
		firstFail       scriptResponse
		wantVerifyCalls int
	}{
		{
			name:            "handler_error",
			firstFail:       scriptResponse{appErr: &common.AppError{Code: 1, Message: "boom"}},
			wantVerifyCalls: 1,
		},
		{
			name:            "unmarshal_error",
			firstFail:       scriptResponse{bytes: []byte{0xff, 0xff}},
			wantVerifyCalls: 1,
		},
		{
			name:            "verify_failure",
			firstFail:       scriptResponse{bytes: wantBytes},
			wantVerifyCalls: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			handler, _ := scriptedHandler(tt.firstFail, scriptResponse{bytes: wantBytes})
			_, tracker := newTestTracker(t, nodeID)
			c := newRetryDispatcher(t, ctx, nodeID, handler, tracker)

			verifyCalls := 0
			parse := func(resp *syncpb.GetLeafResponse) (*syncpb.GetLeafResponse, error) {
				verifyCalls++
				if verifyCalls < tt.wantVerifyCalls {
					return nil, errors.New("invalid")
				}
				return resp, nil
			}

			got, err := c.Send(ctx, &syncpb.GetLeafRequest{}, parse)
			require.NoError(t, err)
			require.Empty(t, cmp.Diff(want, got, protocmp.Transform()))
			require.Len(t, got.GetKeys(), 1) // fresh response per attempt, no merge
			require.Equal(t, tt.wantVerifyCalls, verifyCalls)
		})
	}
}

// Connecting mid-sleep, not before it, proves escalation: a working wait is
// asleep and misses it, noticing only at the next wake-up.
func TestSend_NoPeersBackoffEscalates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		nodeID := ids.GenerateTestNodeID()
		want := &syncpb.GetLeafResponse{Keys: [][]byte{{1, 2, 3}}}
		wantBytes, err := proto.Marshal(want)
		require.NoError(t, err)

		const (
			initial = 30 * time.Millisecond
			factor  = 4.0
			// synctest advances time per timer: 0ms + 120ms + 480ms = 600ms total.
			connectAfter    = 150 * time.Millisecond
			expectedElapsed = 600 * time.Millisecond
		)
		ctx := t.Context()

		handler, _ := scriptedHandler(scriptResponse{bytes: wantBytes})
		_, tracker := newTestTracker(t)
		// The tracker must start empty, so the client is built without telling
		// it about nodeID. The goroutine below connects the peer instead.
		c := &Dispatcher[*syncpb.GetLeafRequest, syncpb.GetLeafResponse, *syncpb.GetLeafResponse, *syncpb.GetLeafResponse]{
			log:    loggingtest.New(t, logging.Debug),
			client: p2ptest.NewSelfTrackingClientWithUnknownPeer(t, ctx, nodeID, handler, tracker),
			peers:  tracker,
		}
		c.policy = testRetryPolicy(
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
		require.Equal(t, expectedElapsed, elapsed,
			"Send should only notice the peer after both no-peers backoffs")
	})
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
		{
			name:     "verify_rejects",
			wantLast: errInvalid,
		},
		{
			name:       "no_peers",
			attemptErr: errNoPeers,
			wantLast:   errNoPeers,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			// verify now runs inside the attempt, so its rejection arrives as
			// the attempt's error.
			attemptErr := tt.attemptErr
			if attemptErr == nil {
				attemptErr = errInvalid
			}
			attempt := func(context.Context) (*syncpb.GetLeafResponse, ids.NodeID, error) {
				cancel()
				return nil, ids.EmptyNodeID, attemptErr
			}

			got, err := doRetry(ctx, loggingtest.New(t, logging.Debug), *defaultRetryPolicy(), attempt)
			require.Nil(t, got)
			require.ErrorIs(t, err, context.Canceled)
			require.ErrorIs(t, err, tt.wantLast)
		})
	}
}

// A fatal classification must stop the loop immediately. attempt
// self-cancels past the first call, so a regression is caught by count.
func TestDoRetry_FatalStopsRetrying(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	calls := 0
	attempt := func(context.Context) (*syncpb.GetLeafResponse, ids.NodeID, error) {
		calls++
		if calls > 1 {
			cancel()
		}
		return nil, ids.EmptyNodeID, context.Canceled
	}

	got, err := doRetry(ctx, loggingtest.New(t, logging.Debug), *defaultRetryPolicy(), attempt)
	require.Nil(t, got)
	require.Equal(t, 1, calls, "doRetry called attempt again after a fatal classification")
	require.ErrorIs(t, err, context.Canceled)
}

// Both [errNoPeers] and any transient network error should prohibit compounding
// of the exponential backoff.
func TestDoRetry_NoPeersStreakResets(t *testing.T) {
	errInvalid := errors.New("invalid")

	tests := []struct {
		name       string
		resetError error // nil: attempt succeeds and verify rejects once instead
	}{
		{
			name:       "peer_scoped_failure",
			resetError: errSendRequest,
		},
		{
			name: "verify_rejection",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const (
					initial         = 100 * time.Millisecond
					factor          = 10.0
					maxBackoff      = 10 * time.Second
					expectedElapsed = 1010 * time.Millisecond // noPeersBackoff(1) + peerFailureBackoff + 0
				)
				policy := testRetryPolicy(
					WithNoPeersInitialBackoff(initial),
					WithNoPeersFactor(factor),
					WithNoPeersMaxBackoff(maxBackoff),
				)

				want := &syncpb.GetLeafResponse{Keys: [][]byte{{1}}}
				calls := 0
				rejected := false
				// call 1,2: no-peers, building backoff.
				// call 3: resetError or verify-rejects, resetting the no-peers count.
				// call 4: no-peers again, should start from the lowest backoff.
				// call 5+: success.
				attempt := func(context.Context) (*syncpb.GetLeafResponse, ids.NodeID, error) {
					calls++
					switch {
					case calls == 1, calls == 2, calls == 4:
						return nil, ids.EmptyNodeID, errNoPeers
					case calls == 3 && tt.resetError != nil:
						return nil, ids.EmptyNodeID, tt.resetError
					case calls == 3 && !rejected:
						// The verify rejection, now reported by the attempt.
						rejected = true
						return nil, ids.GenerateTestNodeID(), errInvalid
					default:
						return want, ids.GenerateTestNodeID(), nil
					}
				}

				start := time.Now()
				got, err := doRetry(
					t.Context(),
					loggingtest.New(t, logging.Debug),
					policy,
					attempt,
				)
				elapsed := time.Since(start)

				require.NoError(t, err)
				require.Empty(t, cmp.Diff(want, got, protocmp.Transform()))
				require.Equal(t, expectedElapsed, elapsed,
					"no-peers count should reset so the fourth attempt starts at backoff(0)")
			})
		})
	}
}

var errRejected = errors.New("rejected by caller")

func rejectLeaf(*syncpb.GetLeafResponse) (*syncpb.GetLeafResponse, error) {
	return nil, errRejected
}

func acceptLeaf(resp *syncpb.GetLeafResponse) (*syncpb.GetLeafResponse, error) {
	return resp, nil
}

func testRetryPolicy(opts ...RetryOption) retryPolicy {
	return *options.ApplyTo(defaultRetryPolicy(), opts...)
}

type leafRetryDispatcher = Dispatcher[*syncpb.GetLeafRequest, syncpb.GetLeafResponse, *syncpb.GetLeafResponse, *syncpb.GetLeafResponse]

func newRetryDispatcher(
	t *testing.T,
	ctx context.Context,
	nodeID ids.NodeID,
	h p2p.Handler,
	tracker *p2p.PeerTracker,
) *leafRetryDispatcher {
	t.Helper()
	c := newTestDispatcher[*syncpb.GetLeafRequest, syncpb.GetLeafResponse, *syncpb.GetLeafResponse, *syncpb.GetLeafResponse](t, ctx, nodeID, h, tracker)
	c.policy = testRetryPolicy(
		WithPeerFailureBackoff(time.Millisecond),
		WithNoPeersInitialBackoff(time.Millisecond),
		WithNoPeersMaxBackoff(5*time.Millisecond),
	)
	return c
}

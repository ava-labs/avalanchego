// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
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
	"github.com/ava-labs/avalanchego/version"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestNoPeersBackoff(t *testing.T) {
	p := *defaultRetryPolicy()

	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{
		{0, 0},
		{1, 15 * time.Millisecond},
		{1000, time.Second},
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
		{"canceled", context.Canceled, retryFatal},
		{"deadline", context.DeadlineExceeded, retryFatal},
		{"wrapped canceled", fmt.Errorf("send: %w", context.Canceled), retryFatal},
		{"marshal", fmt.Errorf("%w: bad", errMarshalRequest), retryFatal},
		{"no peers", errNoPeers, retryNoPeers},
		{"send request", fmt.Errorf("%w: x", errSendRequest), retryPeerScoped},
		{"handler failed", fmt.Errorf("%w: x", errHandlerFailed), retryPeerScoped},
		{"unmarshal response", fmt.Errorf("%w: x", errUnmarshalResponse), retryPeerScoped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classify(tt.err))
		})
	}
}

func TestRetryOptions(t *testing.T) {
	p := *options.ApplyTo(defaultRetryPolicy(),
		WithPeerFailureBackoff(2*time.Second),
		WithNoPeersInitialBackoff(3*time.Second),
		WithNoPeersMaxBackoff(4*time.Second),
		WithNoPeersFactor(5),
	)
	require.Equal(t, 2*time.Second, p.peerFailureBackoff)
	require.Equal(t, 3*time.Second, p.noPeersInitialBackoff)
	require.Equal(t, 4*time.Second, p.noPeersMaxBackoff)
	require.Equal(t, 5.0, p.noPeersFactor)
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
		{"handler error", []scriptResponse{{appErr: &common.AppError{Code: 1, Message: "boom"}}, {bytes: wantBytes}}, false},
		{"unmarshal error", []scriptResponse{{bytes: []byte{0xff, 0xff}}, {bytes: wantBytes}}, false},
		{"verify failure", []scriptResponse{{bytes: wantBytes}, {bytes: wantBytes}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			handler, calls := scriptHandler(tt.responses...)
			_, tracker := newTestTracker(t, nodeID)
			c := newRetryDispatcher(t, ctx, nodeID, handler, tracker)

			verify := acceptLeaf
			if tt.failVerify {
				rejected := false
				verify = func(*syncpb.GetLeafResponse) error {
					if !rejected {
						rejected = true
						return errors.New("invalid")
					}
					return nil
				}
			}

			got, err := c.Send(ctx, &syncpb.GetLeafRequest{}, newLeafResp, verify)
			require.NoError(t, err)
			require.Empty(t, cmp.Diff(want, got, protocmp.Transform()))
			require.Len(t, got.GetKeys(), 1) // fresh response per attempt, no merge
			require.Equal(t, int32(2), calls.Load())
		})
	}
}

func TestSend_NoPeersThenConnect(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	want := &syncpb.GetLeafResponse{Keys: [][]byte{{1, 2, 3}}}
	wantBytes, err := proto.Marshal(want)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	handler, _ := scriptHandler(scriptResponse{bytes: wantBytes})
	_, tracker := newTestTracker(t)
	c := newRetryDispatcher(t, ctx, nodeID, handler, tracker)

	go func() {
		time.Sleep(5 * time.Millisecond)
		tracker.Connected(nodeID, &version.Application{Major: 99})
	}()

	got, err := c.Send(ctx, &syncpb.GetLeafRequest{}, newLeafResp, acceptLeaf)
	require.NoError(t, err)
	require.Empty(t, cmp.Diff(want, got, protocmp.Transform()))
}

func TestSend_CtxCancelledBeforeStart(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	handler, calls := scriptHandler(scriptResponse{bytes: []byte{}})
	_, tracker := newTestTracker(t, nodeID)
	c := newRetryDispatcher(t, ctx, nodeID, handler, tracker)

	got, err := c.Send(ctx, &syncpb.GetLeafRequest{}, newLeafResp, acceptLeaf)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
	require.Zero(t, calls.Load())
}

func newLeafResp() *syncpb.GetLeafResponse { return &syncpb.GetLeafResponse{} }

func acceptLeaf(*syncpb.GetLeafResponse) error { return nil }

func newRetryDispatcher(
	t *testing.T,
	ctx context.Context,
	nodeID ids.NodeID,
	h p2p.Handler,
	tracker *p2p.PeerTracker,
) *Dispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse] {
	t.Helper()
	c := newTestDispatcher[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](t, ctx, nodeID, h, tracker)
	c.policy = *options.ApplyTo(defaultRetryPolicy(),
		WithPeerFailureBackoff(time.Millisecond),
		WithNoPeersInitialBackoff(time.Millisecond),
		WithNoPeersMaxBackoff(5*time.Millisecond),
	)
	return c
}

type scriptResponse struct {
	bytes  []byte
	appErr *common.AppError
}

// scriptHandler replies with each response in order, then repeats the last.
func scriptHandler(responses ...scriptResponse) (p2p.Handler, *atomic.Int32) {
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

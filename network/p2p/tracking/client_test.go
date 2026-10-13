// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracking

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ava-labs/libevm/libevm/options"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

const handlerID = 123

var (
	errFoo = &common.AppError{
		Code:    123,
		Message: "foo",
	}
	errSendFailed = errors.New("send failed")
)

var _ PeerScorer = (*recorder)(nil)

// recorder is a [PeerScorer] that records per peer what a [Client] reports, so
// a test can assert the verdict and bandwidth the client computes.
type recorder struct {
	selected  ids.NodeID
	requested map[ids.NodeID]int
	bandwidth map[ids.NodeID][]float64
	failures  map[ids.NodeID]int
}

func newRecorder(selected ids.NodeID) *recorder {
	return &recorder{
		selected:  selected,
		requested: make(map[ids.NodeID]int),
		bandwidth: make(map[ids.NodeID][]float64),
		failures:  make(map[ids.NodeID]int),
	}
}

func (r *recorder) SelectPeer() (ids.NodeID, bool) { return r.selected, true }

func (r *recorder) RegisterRequest(nodeID ids.NodeID) { r.requested[nodeID]++ }

func (r *recorder) RegisterResponse(nodeID ids.NodeID, bw float64) {
	r.bandwidth[nodeID] = append(r.bandwidth[nodeID], bw)
}

func (r *recorder) RegisterFailure(nodeID ids.NodeID) { r.failures[nodeID]++ }

type (
	sut struct {
		client  *Client
		scorer  *recorder
		network *p2p.Network
		nodeID  ids.NodeID
	}

	sutConfig struct {
		sendErr error
	}
	sutOption = options.Option[sutConfig]
)

// withSendErr fails every send with err.
func withSendErr(err error) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.sendErr = err
	})
}

// newSUT returns a [Client] over one connected peer, and the [recorder] it
// scores against.
func newSUT(t *testing.T, opts ...sutOption) *sut {
	t.Helper()

	cfg := options.ApplyTo(&sutConfig{}, opts...)

	sender := &enginetest.Sender{
		SendAppRequestF: func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
			return cfg.sendErr
		},
	}
	// A failed send is logged at Error, which loggingtest turns into a test
	// failure, and one case here provokes exactly that.
	network, err := p2p.NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(t, err)

	nodeID := ids.GenerateTestNodeID()
	scorer := newRecorder(nodeID)
	return &sut{
		client:  NewClient(network, handlerID, scorer),
		scorer:  scorer,
		network: network,
		nodeID:  nodeID,
	}
}

func TestClientScoresRequests(t *testing.T) {
	tests := []struct {
		name            string
		sendErr         error
		appErr          *common.AppError
		response        []byte
		callbackRejects bool
		preCancel       bool

		wantFailure bool
	}{
		{
			name:     "response_accepted",
			response: []byte("response"),
		},
		{
			name:            "response_rejected_by_caller",
			response:        []byte("response"),
			callbackRejects: true,
			wantFailure:     true,
		},
		{
			name:        "request_failed",
			appErr:      errFoo,
			wantFailure: true,
		},
		{
			// Registration precedes the send, so a send that never left the
			// node is balanced by a failure rather than left outstanding.
			name:        "send_failed",
			sendErr:     errSendFailed,
			wantFailure: true,
		},
		{
			name:      "cancelled_before_send_response_still_accepted",
			response:  []byte("response"),
			preCancel: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()

			var opts []sutOption
			if test.sendErr != nil {
				opts = append(opts, withSendErr(test.sendErr))
			}
			s := newSUT(t, opts...)

			requestCtx := ctx
			if test.preCancel {
				var cancel context.CancelFunc
				requestCtx, cancel = context.WithCancel(ctx)
				cancel()
			}

			err := s.client.AppRequest(
				requestCtx,
				set.Of(s.nodeID),
				[]byte("request"),
				func(context.Context, ids.NodeID, []byte, error) bool {
					return !test.callbackRejects
				},
			)

			// The router runs the callback inline, so scoring is complete once
			// the delivery below returns.
			switch {
			case test.sendErr != nil:
				require.ErrorIs(t, err, test.sendErr)
			case test.appErr != nil:
				require.NoError(t, err)
				require.NoError(t, s.network.AppRequestFailed(ctx, s.nodeID, 1, test.appErr))
			default:
				require.NoError(t, err)
				require.NoError(t, s.network.AppResponse(ctx, s.nodeID, 1, test.response))
			}

			require.Equal(t, 1, s.scorer.requested[s.nodeID], "RegisterRequest()")
			if test.wantFailure {
				require.Equal(t, 1, s.scorer.failures[s.nodeID], "RegisterFailure()")
				require.Empty(t, s.scorer.bandwidth[s.nodeID], "RegisterResponse()")
				return
			}
			require.Zero(t, s.scorer.failures[s.nodeID], "RegisterFailure()")
			require.Len(t, s.scorer.bandwidth[s.nodeID], 1, "RegisterResponse()")
		})
	}
}

// Bandwidth measures delivery, so a caller that takes a long time to verify a
// response must not make the peer that served it look slow.
func TestClientScoresBandwidthBeforeVerification(t *testing.T) {
	const verification = 50 * time.Millisecond

	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		s := newSUT(t)
		response := []byte("response")

		require.NoError(t, s.client.AppRequest(
			ctx,
			set.Of(s.nodeID),
			[]byte("request"),
			func(context.Context, ids.NodeID, []byte, error) bool {
				time.Sleep(verification)
				return true
			},
		))
		require.NoError(t, s.network.AppResponse(ctx, s.nodeID, 1, response))

		// Fake time does not advance before the callback, so a measurement
		// taken first divides by bandwidthEpsilon alone.
		want := float64(len(response)) / bandwidthEpsilon
		require.Equal(t, []float64{want}, s.scorer.bandwidth[s.nodeID])
	})
}

func TestClientScoresEachPeer(t *testing.T) {
	ctx := t.Context()

	// Sends run inline within AppRequest, so this needs no synchronization.
	routedTo := map[uint32]ids.NodeID{}
	sender := &enginetest.Sender{
		SendAppRequestF: func(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, _ []byte) error {
			for nodeID := range nodeIDs {
				routedTo[requestID] = nodeID
			}
			return nil
		},
	}

	network, err := p2p.NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(t, err)

	accepted := ids.GenerateTestNodeID()
	rejected := ids.GenerateTestNodeID()

	scorer := newRecorder(accepted)
	client := NewClient(network, handlerID, scorer)

	require.NoError(t, client.AppRequest(
		ctx,
		set.Of(accepted, rejected),
		[]byte("request"),
		func(_ context.Context, nodeID ids.NodeID, _ []byte, _ error) bool {
			return nodeID != rejected
		},
	))
	require.Len(t, routedTo, 2)

	for requestID, nodeID := range routedTo {
		require.NoError(t, network.AppResponse(ctx, nodeID, requestID, []byte("response")))
	}

	require.Len(t, scorer.bandwidth[accepted], 1, "accepted peer scored responsive")
	require.Zero(t, scorer.failures[accepted], "accepted peer not failed")
	require.Empty(t, scorer.bandwidth[rejected], "rejected peer not scored responsive")
	require.Equal(t, 1, scorer.failures[rejected], "rejected peer failed")
}

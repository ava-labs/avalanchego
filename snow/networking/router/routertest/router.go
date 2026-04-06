// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package routertest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
)

// RegisteredRequest records the arguments of a single RegisterRequest call.
type RegisteredRequest struct {
	NodeID     ids.NodeID
	ChainID    ids.ID
	RequestID  uint32
	Op         message.Op
	FailedMsg  *message.InboundMessage
	EngineType p2p.EngineType
}

// Router wraps a real ChainRouter and records messages passed to
// HandleInternal and RegisterRequest so tests can assert on their content.
type Router struct {
	*router.ChainRouter

	mu                 sync.Mutex
	InternalMessages   []*message.InboundMessage
	RegisteredRequests []RegisteredRequest
}

func (r *Router) HandleInternal(ctx context.Context, msg *message.InboundMessage) {
	r.mu.Lock()
	r.InternalMessages = append(r.InternalMessages, msg)
	r.mu.Unlock()

	r.ChainRouter.HandleInternal(ctx, msg)
}

func (r *Router) RegisterRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	failedMsg *message.InboundMessage,
	engineType p2p.EngineType,
) {
	r.mu.Lock()
	r.RegisteredRequests = append(r.RegisteredRequests, RegisteredRequest{
		NodeID:     nodeID,
		ChainID:    chainID,
		RequestID:  requestID,
		Op:         op,
		FailedMsg:  failedMsg,
		EngineType: engineType,
	})
	r.mu.Unlock()

	r.ChainRouter.RegisterRequest(ctx, nodeID, chainID, requestID, op, failedMsg, engineType)
}

// PopInternalMessage removes and returns the oldest captured internal message.
// It fails the test if no messages are available.
func (r *Router) PopInternalMessage(t testing.TB) *message.InboundMessage {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	require.NotEmpty(t, r.InternalMessages, "expected an internal message")
	msg := r.InternalMessages[0]
	r.InternalMessages = r.InternalMessages[1:]
	return msg
}

// PopRegisteredRequest removes and returns the oldest captured RegisterRequest call.
// It fails the test if no requests are available.
func (r *Router) PopRegisteredRequest(t testing.TB) RegisteredRequest {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	require.NotEmpty(t, r.RegisteredRequests, "expected a registered request")
	req := r.RegisteredRequests[0]
	r.RegisteredRequests = r.RegisteredRequests[1:]
	return req
}

// New creates a new initialized Router (wrapping ChainRouter) for testing
// with its own timeout manager. The timeout manager is automatically
// cleaned up when the test finishes.
func New(t testing.TB) *Router {
	tm, err := timeout.NewManager(
		&timer.AdaptiveTimeoutConfig{
			InitialTimeout:     time.Millisecond,
			MinimumTimeout:     time.Millisecond,
			MaximumTimeout:     10 * time.Second,
			TimeoutCoefficient: 1.25,
			TimeoutHalflife:    5 * time.Minute,
		},
		benchlist.NewNoBenchlist(),
		prometheus.NewRegistry(),
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)

	go tm.Dispatch()
	t.Cleanup(tm.Stop)

	chainRouter := &router.ChainRouter{}
	require.NoError(t, chainRouter.Initialize(
		ids.EmptyNodeID,
		logging.NoLog{},
		tm,
		time.Second,
		set.Set[ids.ID]{},
		true,
		set.Set[ids.ID]{},
		nil,
		router.HealthConfig{},
		prometheus.NewRegistry(),
	))
	return &Router{ChainRouter: chainRouter}
}

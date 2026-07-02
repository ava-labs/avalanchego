// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var _ p2p.Handler = (*ScriptedHandler)(nil)

// ScriptedHandler replies with a preset sequence of responses, one per
// request. A nil entry is served as an AppError to exercise the
// transport-failure retry path. Once the script is exhausted it keeps
// serving the last response so extra retries don't stall.
type ScriptedHandler struct {
	lock      sync.Mutex
	responses [][]byte
	n         int
}

// NewScriptedHandler returns a [ScriptedHandler] that serves responses in
// order. A nil entry is served as an AppError.
func NewScriptedHandler(responses [][]byte) *ScriptedHandler {
	return &ScriptedHandler{responses: responses}
}

func (h *ScriptedHandler) AppRequest(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
	h.lock.Lock()
	defer h.lock.Unlock()

	idx := h.n
	if idx >= len(h.responses) {
		idx = len(h.responses) - 1
	}
	h.n++

	resp := h.responses[idx]
	if resp == nil {
		return nil, &common.AppError{Code: 1, Message: "boom"}
	}
	return resp, nil
}

func (*ScriptedHandler) AppGossip(context.Context, ids.NodeID, []byte) {}

// Calls returns the number of times AppRequest has been invoked.
func (h *ScriptedHandler) Calls() int {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.n
}

// NewLoopbackNetwork builds a single-node [p2p.Network] whose outbound app
// messages loop straight back inbound via an [enginetest.Sender], with h
// registered at handlerID and one connected peer registered in the returned
// [p2p.PeerTracker]. It is ready for a client bound to handlerID: a request
// sent to the tracked peer is served by h and its response (or error) is
// routed back to the caller.
func NewLoopbackNetwork(t *testing.T, handlerID uint64, h p2p.Handler) (*p2p.Network, *p2p.PeerTracker) {
	t.Helper()
	ctx := t.Context()
	nodeID := ids.GenerateTestNodeID()

	sender := &enginetest.Sender{}
	log := loggingtest.New(t, logging.Debug)
	net, err := p2p.NewNetwork(log, sender, prometheus.NewRegistry(), "")
	require.NoError(t, err)

	// Loop the request back into the same network as an inbound AppRequest,
	// and route the response/error back as the matching inbound message.
	sender.SendAppRequestF = func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
		for range nodeIDs {
			go func() {
				_ = net.AppRequest(ctx, nodeID, requestID, time.Time{}, requestBytes)
			}()
		}
		return nil
	}
	sender.SendAppResponseF = func(ctx context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
		go func() {
			_ = net.AppResponse(ctx, nodeID, requestID, responseBytes)
		}()
		return nil
	}
	sender.SendAppErrorF = func(ctx context.Context, _ ids.NodeID, requestID uint32, code int32, msg string) error {
		go func() {
			_ = net.AppRequestFailed(ctx, nodeID, requestID, &common.AppError{Code: code, Message: msg})
		}()
		return nil
	}

	tracker, err := p2p.NewPeerTracker(log, "test", prometheus.NewRegistry(), nil, nil)
	require.NoError(t, err)

	require.NoError(t, net.AddHandler(handlerID, h))
	require.NoError(t, net.Connected(ctx, nodeID, nil))
	tracker.Connected(nodeID, &version.Application{Major: 99})

	return net, tracker
}

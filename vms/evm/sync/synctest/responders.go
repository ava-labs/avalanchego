// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
)

// ServeResponder registers r at handlerID on a single-node loopback network.
func ServeResponder[V any, Req handlers.ProtoMessage[V], Resp proto.Message](
	t *testing.T,
	ctx context.Context,
	log logging.Logger,
	handlerID uint64,
	r handlers.Responder[Req, Resp],
) (*p2p.Network, *p2p.PeerTracker) {
	t.Helper()

	net, tracker := NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, net.AddHandler(handlerID, handlers.NewHandler(log, r)))
	return net, tracker
}

// RecordingResponder records every request reaching inner.
type RecordingResponder[Req, Resp proto.Message] struct {
	inner handlers.Responder[Req, Resp]

	lock     sync.Mutex
	requests []Req
}

func NewRecordingResponder[Req, Resp proto.Message](inner handlers.Responder[Req, Resp]) *RecordingResponder[Req, Resp] {
	return &RecordingResponder[Req, Resp]{inner: inner}
}

func (c *RecordingResponder[Req, Resp]) Respond(ctx context.Context, nodeID ids.NodeID, req Req) (Resp, *common.AppError) {
	c.lock.Lock()
	c.requests = append(c.requests, req)
	c.lock.Unlock()
	return c.inner.Respond(ctx, nodeID, req)
}

// Requests returns the requests served so far, in arrival order.
func (c *RecordingResponder[Req, Resp]) Requests() []Req {
	c.lock.Lock()
	defer c.lock.Unlock()
	return slices.Clone(c.requests)
}

// MutatingResponder corrupts the first bad responses from inner, zero corrupts
// none.
//
// Tampering with well-formed responses, rather than erroring, leaves the client's
// own verification as the thing under test.
type MutatingResponder[Req, Resp proto.Message] struct {
	inner  handlers.Responder[Req, Resp]
	mutate func(Resp)
	bad    int

	served atomic.Int32
}

func NewMutatingResponder[Req, Resp proto.Message](
	inner handlers.Responder[Req, Resp],
	bad int,
	mutate func(Resp),
) *MutatingResponder[Req, Resp] {
	return &MutatingResponder[Req, Resp]{inner: inner, mutate: mutate, bad: bad}
}

func (m *MutatingResponder[Req, Resp]) Respond(ctx context.Context, nodeID ids.NodeID, req Req) (Resp, *common.AppError) {
	resp, appErr := m.inner.Respond(ctx, nodeID, req)
	if appErr != nil {
		return resp, appErr
	}
	if served := int(m.served.Add(1)); served <= m.bad {
		m.mutate(resp)
	}
	return resp, nil
}

// CancelAfter cancels once the at-th request arrives, ending a sync that would
// otherwise never converge.
type CancelAfter[Req, Resp proto.Message] struct {
	inner  handlers.Responder[Req, Resp]
	cancel context.CancelFunc
	at     int

	seen atomic.Int32
}

func NewCancelAfter[Req, Resp proto.Message](
	inner handlers.Responder[Req, Resp],
	at int,
	cancel context.CancelFunc,
) *CancelAfter[Req, Resp] {
	return &CancelAfter[Req, Resp]{inner: inner, cancel: cancel, at: at}
}

func (c *CancelAfter[Req, Resp]) Respond(ctx context.Context, nodeID ids.NodeID, req Req) (Resp, *common.AppError) {
	if c.reached(int(c.seen.Add(1))) {
		c.cancel()
	}
	return c.inner.Respond(ctx, nodeID, req)
}

// reached reports whether seen requests is enough to cancel.
func (c *CancelAfter[Req, Resp]) reached(seen int) bool {
	return seen >= c.at
}

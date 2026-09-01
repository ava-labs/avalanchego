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

// NewRecordingResponder returns a [RecordingResponder] wrapping inner.
func NewRecordingResponder[Req, Resp proto.Message](inner handlers.Responder[Req, Resp]) *RecordingResponder[Req, Resp] {
	return &RecordingResponder[Req, Resp]{inner: inner}
}

func (r *RecordingResponder[Req, Resp]) Respond(ctx context.Context, nodeID ids.NodeID, req Req) (Resp, *common.AppError) {
	r.lock.Lock()
	r.requests = append(r.requests, req)
	r.lock.Unlock()
	return r.inner.Respond(ctx, nodeID, req)
}

// Requests returns the requests served so far, in arrival order.
func (r *RecordingResponder[Req, Resp]) Requests() []Req {
	r.lock.Lock()
	defer r.lock.Unlock()
	return slices.Clone(r.requests)
}

// MutatingResponder mutates the first numBad well-formed responses from inner.
//
// Tampering with well-formed responses, rather than erroring, leaves the client's
// own verification as the thing under test.
type MutatingResponder[Req, Resp proto.Message] struct {
	inner  handlers.Responder[Req, Resp]
	mutate func(Resp)
	numBad int

	served atomic.Int32
}

// NewMutatingResponder returns a [MutatingResponder] that corrupts the first
// numBad responses with mutate.
func NewMutatingResponder[Req, Resp proto.Message](
	inner handlers.Responder[Req, Resp],
	numBad int,
	mutate func(Resp),
) *MutatingResponder[Req, Resp] {
	return &MutatingResponder[Req, Resp]{inner: inner, mutate: mutate, numBad: numBad}
}

func (m *MutatingResponder[Req, Resp]) Respond(ctx context.Context, nodeID ids.NodeID, req Req) (Resp, *common.AppError) {
	resp, appErr := m.inner.Respond(ctx, nodeID, req)
	if appErr != nil {
		return resp, appErr
	}
	if served := int(m.served.Add(1)); served <= m.numBad {
		m.mutate(resp)
	}
	return resp, nil
}

// ErroringResponder rejects the first numBad requests after reaching inner,
// mirroring a peer that cannot serve the request.
type ErroringResponder[Req, Resp proto.Message] struct {
	inner  handlers.Responder[Req, Resp]
	err    *common.AppError
	numBad int

	served atomic.Int32
}

// NewErroringResponder returns an [ErroringResponder] that rejects the first
// numBad requests with err.
func NewErroringResponder[Req, Resp proto.Message](
	inner handlers.Responder[Req, Resp],
	numBad int,
	err *common.AppError,
) *ErroringResponder[Req, Resp] {
	return &ErroringResponder[Req, Resp]{inner: inner, numBad: numBad, err: err}
}

func (e *ErroringResponder[Req, Resp]) Respond(ctx context.Context, nodeID ids.NodeID, req Req) (Resp, *common.AppError) {
	resp, appErr := e.inner.Respond(ctx, nodeID, req)
	if served := int(e.served.Add(1)); served <= e.numBad {
		var zero Resp
		return zero, e.err
	}
	return resp, appErr
}

// CancelAfter cancels once the at-th request arrives, ending a sync that would
// otherwise never converge.
type CancelAfter[Req, Resp proto.Message] struct {
	inner  handlers.Responder[Req, Resp]
	cancel context.CancelFunc
	at     int

	seen atomic.Int32
}

// NewCancelAfter returns a [CancelAfter] that calls cancel once at requests
// have arrived.
func NewCancelAfter[Req, Resp proto.Message](
	inner handlers.Responder[Req, Resp],
	at int,
	cancel context.CancelFunc,
) *CancelAfter[Req, Resp] {
	return &CancelAfter[Req, Resp]{inner: inner, cancel: cancel, at: at}
}

func (c *CancelAfter[Req, Resp]) Respond(ctx context.Context, nodeID ids.NodeID, req Req) (Resp, *common.AppError) {
	resp, appErr := c.inner.Respond(ctx, nodeID, req)
	if int(c.seen.Add(1)) >= c.at {
		c.cancel()
	}
	return resp, appErr
}

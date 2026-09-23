// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/libevm/options"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	errNoPeers           = errors.New("no peers available")
	errSendRequest       = errors.New("send request")
	errHandlerFailed     = errors.New("handler request failed")
	errMarshalRequest    = errors.New("marshal request")
	errUnmarshalResponse = errors.New("unmarshal response")
)

// ProtoMessage constrains a message to its pointer type, so a generic holder can
// allocate one with new instead of taking a constructor.
type ProtoMessage[T any] interface {
	proto.Message
	*T
}

// Dispatcher is a typed synchronous client bound to one handler ID.
// Use one instance per RPC type.
type Dispatcher[Req proto.Message, In any, Resp ProtoMessage[In], Out any] struct {
	log    logging.Logger
	client *p2p.TrackingClient
	peers  *p2p.PeerTracker
	policy retryPolicy
}

// NewDispatcher returns a [Dispatcher] bound to handlerID on n.
func NewDispatcher[Req proto.Message, In any, Resp ProtoMessage[In], Out any](
	log logging.Logger,
	n *p2p.Network,
	handlerID uint64,
	peers *p2p.PeerTracker,
	opts ...RetryOption,
) *Dispatcher[Req, In, Resp, Out] {
	return &Dispatcher[Req, In, Resp, Out]{
		// Tagged once, so every retry line names the RPC without each caller repeating it.
		log:    log.With(zap.Uint64("handlerID", handlerID)),
		client: n.NewTrackingClient(handlerID, peers),
		peers:  peers,
		policy: *options.ApplyTo(defaultRetryPolicy(), opts...),
	}
}

// Send retries req through [Dispatcher.SendTo] until verify accepts a response
// or ctx ends. req is marshaled once since it never changes across attempts,
// and verify names the rejecting peer and returns Send's result.
func (d *Dispatcher[Req, In, Resp, Out]) Send(
	ctx context.Context,
	req Req,
	verify func(Resp, ids.NodeID) (Out, error),
) (Out, error) {
	requestBytes, err := proto.Marshal(req)
	if err != nil {
		var zero Out
		return zero, fmt.Errorf("%w: %w", errMarshalRequest, err)
	}
	return doRetry(ctx, d.log, d.policy, func() (Out, ids.NodeID, error) {
		nodeID, ok := d.peers.SelectPeer()
		if !ok {
			var zero Out
			return zero, ids.EmptyNodeID, errNoPeers
		}
		out, err := d.sendBytes(ctx, nodeID, requestBytes, verify)
		return out, nodeID, err
	})
}

// SendTo sends req to nodeID and returns what verify made of the reply.
// A pre-send context or marshal error returns before the peer is registered.
func (d *Dispatcher[Req, In, Resp, Out]) SendTo(
	ctx context.Context,
	nodeID ids.NodeID,
	req Req,
	verify func(Resp, ids.NodeID) (Out, error),
) (Out, error) {
	var zero Out
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	requestBytes, err := proto.Marshal(req)
	if err != nil {
		return zero, fmt.Errorf("%w: %w", errMarshalRequest, err)
	}
	return d.sendBytes(ctx, nodeID, requestBytes, verify)
}

// sendBytes is [Dispatcher.SendTo] past the marshal step, shared with
// [Dispatcher.Send]'s retry loop so a retried request is marshaled once, not
// once per attempt.
func (d *Dispatcher[Req, In, Resp, Out]) sendBytes(
	ctx context.Context,
	nodeID ids.NodeID,
	requestBytes []byte,
	verify func(Resp, ids.NodeID) (Out, error),
) (Out, error) {
	var zero Out
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	type result struct {
		out Out
		err error
	}
	// Closed before verify runs, so a cancelled caller can tell a reply being
	// verified from one that never came.
	arrived := make(chan struct{})
	// Buffered so a reply landing after ctx ends never blocks the handler.
	resultCh := make(chan result, 1)
	onResponse := func(_ context.Context, respNodeID ids.NodeID, responseBytes []byte, appErr error) error {
		close(arrived)
		out, err := decode[In, Resp](respNodeID, responseBytes, appErr, verify)
		resultCh <- result{out: out, err: err}
		return err
	}

	if err := d.client.AppRequest(ctx, set.Of(nodeID), requestBytes, onResponse); err != nil {
		return zero, fmt.Errorf("%w: %w", errSendRequest, err)
	}

	select {
	case r := <-resultCh:
		return r.out, r.err
	case <-ctx.Done():
		select {
		case <-arrived:
			r := <-resultCh
			return r.out, r.err
		default:
			return zero, ctx.Err()
		}
	}
}

// decode turns a reply into Resp and applies the caller's verdict. It runs on
// the handler goroutine, so a rejection de-scores the peer that served it.
func decode[In any, Resp ProtoMessage[In], Out any](
	nodeID ids.NodeID,
	responseBytes []byte,
	appErr error,
	verify func(Resp, ids.NodeID) (Out, error),
) (Out, error) {
	var zero Out
	if appErr != nil {
		return zero, fmt.Errorf("%w: %w", errHandlerFailed, appErr)
	}
	resp := Resp(new(In))
	if err := proto.Unmarshal(responseBytes, resp); err != nil {
		return zero, fmt.Errorf("%w: %w", errUnmarshalResponse, err)
	}
	return verify(resp, nodeID)
}

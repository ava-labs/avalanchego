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

// Send retries req until a peer sends a response that parse accepts or ctx
// ends. req is marshaled once since it never changes across attempts.
func (d *Dispatcher[Req, In, Resp, Out]) Send(
	ctx context.Context,
	req Req,
	parse func(Resp) (Out, error),
) (Out, error) {
	requestBytes, err := proto.Marshal(req)
	if err != nil {
		var zero Out
		return zero, fmt.Errorf("%w: %w", errMarshalRequest, err)
	}
	return doRetry(ctx, d.log, d.policy, func(ctx context.Context) (Out, error) {
		nodeID, ok := d.peers.SelectPeer()
		if !ok {
			var zero Out
			return zero, errNoPeers
		}
		return d.sendBytes(ctx, nodeID, requestBytes, parse)
	})
}

func (d *Dispatcher[Req, In, Resp, Out]) sendBytes(
	ctx context.Context,
	nodeID ids.NodeID,
	requestBytes []byte,
	parse func(Resp) (Out, error),
) (Out, error) {
	var zero Out
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	type result struct {
		out Out
		err error
	}
	// Buffered so a reply landing after ctx ends never blocks the handler.
	resultCh := make(chan result, 1)
	onResponse := func(_ context.Context, _ ids.NodeID, responseBytes []byte, appErr error) error {
		out, err := decode[In, Resp](responseBytes, appErr, parse)
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
		return zero, ctx.Err()
	}
}

// decode turns a reply into Resp and applies the caller's verdict. It runs on
// the handler goroutine, so a rejection de-scores the peer that served it.
func decode[In any, Resp ProtoMessage[In], Out any](
	responseBytes []byte,
	appErr error,
	parse func(Resp) (Out, error),
) (Out, error) {
	var zero Out
	if appErr != nil {
		return zero, fmt.Errorf("%w: %w", errHandlerFailed, appErr)
	}
	resp := Resp(new(In))
	if err := proto.Unmarshal(responseBytes, resp); err != nil {
		return zero, fmt.Errorf("%w: %w", errUnmarshalResponse, err)
	}
	return parse(resp)
}

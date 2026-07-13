// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/libevm/libevm/options"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	errNoPeers           = errors.New("no peers available")
	errSendRequest       = errors.New("send request")
	errHandlerFailed     = errors.New("handler request failed")
	errMarshalRequest    = errors.New("marshal request")
	errUnmarshalResponse = errors.New("unmarshal response")
)

// Dispatcher is a typed synchronous client bound to one handler ID.
// Use one instance per RPC type.
type Dispatcher[Req, Resp proto.Message] struct {
	client *p2p.Client
	peers  *p2p.PeerTracker
	policy retryPolicy
}

// NewDispatcher returns a [Dispatcher] bound to handlerID on n.
func NewDispatcher[Req, Resp proto.Message](
	n *p2p.Network,
	handlerID uint64,
	peers *p2p.PeerTracker,
	opts ...RetryOption,
) *Dispatcher[Req, Resp] {
	return &Dispatcher[Req, Resp]{
		client: n.NewClient(handlerID, noopSampler{}),
		peers:  peers,
		policy: *options.ApplyTo(defaultRetryPolicy(), opts...),
	}
}

// Send retries req through [SendTo] until verify accepts a response or ctx ends.
// Each attempt gets a fresh Resp from newResp, so a failed one never merges into
// the next.
func (d *Dispatcher[Req, Resp]) Send(
	ctx context.Context,
	req Req,
	newResp func() Resp,
	verify func(Resp) error,
) (Resp, error) {
	return doRetry(ctx, d.policy, verify, func() (Resp, *Outcome, error) {
		nodeID, ok := d.peers.SelectPeer()
		if !ok {
			var zero Resp
			return zero, nil, errNoPeers
		}
		resp := newResp()
		outcome, err := d.SendTo(ctx, nodeID, req, resp)
		return resp, outcome, err
	})
}

// SendTo sends req to nodeID. A pre-send context or marshal error
// returns unscored, any later failure scores the peer and returns a nil
// Outcome.
func (d *Dispatcher[Req, Resp]) SendTo(ctx context.Context, nodeID ids.NodeID, req Req, resp Resp) (_ *Outcome, retErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requestBytes, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errMarshalRequest, err)
	}

	d.peers.RegisterRequest(nodeID)
	defer func() {
		if retErr != nil {
			d.peers.RegisterFailure(nodeID)
		}
	}()

	type result struct {
		bytes []byte
		err   error
	}
	resultCh := make(chan result, 1)
	onResponse := func(_ context.Context, _ ids.NodeID, responseBytes []byte, err error) {
		resultCh <- result{bytes: responseBytes, err: err}
	}

	start := time.Now()
	if err := d.client.AppRequest(ctx, set.Of(nodeID), requestBytes, onResponse); err != nil {
		return nil, fmt.Errorf("%w: %w", errSendRequest, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-resultCh:
		if r.err != nil {
			return nil, fmt.Errorf("%w: %w", errHandlerFailed, r.err)
		}

		if err := proto.Unmarshal(r.bytes, resp); err != nil {
			return nil, fmt.Errorf("%w: %w", errUnmarshalResponse, err)
		}
		const epsilon = 1e-6
		bandwidth := float64(len(r.bytes)) / (time.Since(start).Seconds() + epsilon)
		return &Outcome{
			peers:     d.peers,
			nodeID:    nodeID,
			bandwidth: bandwidth,
		}, nil
	}
}

// Outcome scores a peer after the caller validates its response. Call at
// least one of Success or Failure. Both are idempotent, so a pessimistic
// defer Failure() with Success() on the happy path is safe. Forgetting
// both leaks the peer's RegisterRequest.
type Outcome struct {
	peers     *p2p.PeerTracker
	nodeID    ids.NodeID
	bandwidth float64
	once      sync.Once
}

// Success records the response as semantically valid.
func (o *Outcome) Success() {
	o.once.Do(func() { o.peers.RegisterResponse(o.nodeID, o.bandwidth) })
}

// Failure records the response as semantically invalid.
func (o *Outcome) Failure() {
	o.once.Do(func() { o.peers.RegisterFailure(o.nodeID) })
}

var _ p2p.NodeSampler = noopSampler{}

// noopSampler satisfies [p2p.Network.NewClient]'s non-nil sampler
// requirement. [Dispatcher] always picks an explicit peer, so Sample
// never runs.
type noopSampler struct{}

func (noopSampler) Sample(context.Context, int) []ids.NodeID { return nil }

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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
}

// NewDispatcher returns a typed [Dispatcher] over client and peers.
// Build client via [NewClient] in production.
func NewDispatcher[Req, Resp proto.Message](
	client *p2p.Client,
	peers *p2p.PeerTracker,
) *Dispatcher[Req, Resp] {
	return &Dispatcher[Req, Resp]{client: client, peers: peers}
}

// NewClient returns a [p2p.Client] at handlerID on n. The sampler is a
// no-op because [Dispatcher] always picks an explicit peer.
func NewClient(n *p2p.Network, handlerID uint64) *p2p.Client {
	return n.NewClient(handlerID, noopSampler{})
}

// Send selects a peer and forwards req to it. Outcome is nil on error.
// With no peer available it returns errNoPeers unscored, otherwise it
// scores per [SendTo].
func (d *Dispatcher[Req, Resp]) Send(ctx context.Context, req Req, resp Resp) (*Outcome, error) {
	nodeID, ok := d.peers.SelectPeer()
	if !ok {
		return nil, errNoPeers
	}
	return d.SendTo(ctx, nodeID, req, resp)
}

// SendTo sends req to nodeID. Outcome is nil on error. The peer is
// scored a failure only after the request is registered, so a pre-send
// marshal or context error returns unscored.
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

// Outcome lets the caller score a peer after validating its response.
// At least one of Success or Failure must be called. Both are
// idempotent, so defer outcome.Failure() plus outcome.Success() is
// safe. Forgetting both leaves an unpaired RegisterRequest on the
// [p2p.PeerTracker].
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

// noopSampler is a no-op [p2p.NodeSampler]. Required because
// [p2p.Network.NewClient] needs a non-nil sampler, but [Dispatcher]
// always picks an explicit peer so Sample never runs.
type noopSampler struct{}

func (noopSampler) Sample(context.Context, int) []ids.NodeID { return nil }

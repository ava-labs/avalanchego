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

const epsilon = 1e-6

var (
	errNoPeers           = errors.New("no peers available")
	errSendRequest       = errors.New("send request")
	errHandlerFailed     = errors.New("handler request failed")
	errMarshalRequest    = errors.New("marshal request")
	errUnmarshalResponse = errors.New("unmarshal response")
)

// Dispatcher is a typed synchronous client for one handler ID. Safe for
// concurrent use. Instantiate against a new handler ID to add an RPC type.
type Dispatcher[Req, Resp proto.Message] struct {
	client *p2p.Client
	peers  *p2p.PeerTracker
}

// NewDispatcher binds a [p2p.Client] at handlerID using peers for
// selection and bandwidth scoring.
func NewDispatcher[Req, Resp proto.Message](
	p2pNet *p2p.Network,
	handlerID uint64,
	peers *p2p.PeerTracker,
) *Dispatcher[Req, Resp] {
	return &Dispatcher[Req, Resp]{
		client: p2pNet.NewClient(handlerID, trackerSampler{peers: peers}),
		peers:  peers,
	}
}

// Send picks a peer, sends req, unmarshals into resp. Returns the chosen
// nodeID and an Outcome the caller must mark after content validation.
// Selection is explicit (not AppRequestAny) so RegisterRequest runs
// before sending and prevents concurrent picks of the same peer.
//
// On transport-level failure (no peers, send error, ctx, unmarshal),
// Outcome is nil and the peer has already been marked as a failure.
func (d *Dispatcher[Req, Resp]) Send(ctx context.Context, req Req, resp Resp) (ids.NodeID, *Outcome, error) {
	nodeID, ok := d.peers.SelectPeer()
	if !ok {
		return ids.EmptyNodeID, nil, errNoPeers
	}
	outcome, err := d.SendTo(ctx, nodeID, req, resp)
	if err != nil {
		return ids.EmptyNodeID, nil, err
	}
	return nodeID, outcome, nil
}

// SendTo sends req to an explicit peer. Same Outcome contract as Send.
func (d *Dispatcher[Req, Resp]) SendTo(ctx context.Context, nodeID ids.NodeID, req Req, resp Resp) (*Outcome, error) {
	requestBytes, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errMarshalRequest, err)
	}
	return d.dispatch(ctx, nodeID, requestBytes, resp)
}

// dispatch is the inner send-and-await cycle.
func (d *Dispatcher[Req, Resp]) dispatch(ctx context.Context, nodeID ids.NodeID, requestBytes []byte, resp Resp) (*Outcome, error) {
	d.peers.RegisterRequest(nodeID)

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
		d.peers.RegisterFailure(nodeID)
		return nil, fmt.Errorf("%w: %w", errSendRequest, err)
	}

	select {
	case <-ctx.Done():
		d.peers.RegisterFailure(nodeID)
		return nil, ctx.Err()
	case r := <-resultCh:
		if r.err != nil {
			d.peers.RegisterFailure(nodeID)
			return nil, fmt.Errorf("%w: %w", errHandlerFailed, r.err)
		}

		bandwidth := float64(len(r.bytes)) / (time.Since(start).Seconds() + epsilon)
		if err := proto.Unmarshal(r.bytes, resp); err != nil {
			d.peers.RegisterFailure(nodeID)
			return nil, fmt.Errorf("%w: %w", errUnmarshalResponse, err)
		}
		return &Outcome{
			peers:     d.peers,
			nodeID:    nodeID,
			bandwidth: bandwidth,
		}, nil
	}
}

// Outcome defers peer scoring to the caller, who knows whether the
// response is semantically valid. Exactly one of Success or Failure
// must be called per response. Forgetting both leaves the peer with a
// pending RegisterRequest until their next request or disconnect.
type Outcome struct {
	peers     *p2p.PeerTracker
	nodeID    ids.NodeID
	bandwidth float64
	once      sync.Once
}

// Success records the response as semantically valid using the
// bandwidth measured at dispatch time.
func (o *Outcome) Success() {
	if o == nil {
		return
	}
	o.once.Do(func() { o.peers.RegisterResponse(o.nodeID, o.bandwidth) })
}

// Failure records the response as semantically invalid.
func (o *Outcome) Failure() {
	if o == nil {
		return
	}
	o.once.Do(func() { o.peers.RegisterFailure(o.nodeID) })
}

var _ p2p.NodeSampler = (*trackerSampler)(nil)

// trackerSampler is a defensive fallback for AppRequestAny on the
// underlying [p2p.Client]. Dispatcher itself never uses it.
type trackerSampler struct {
	peers *p2p.PeerTracker
}

func (s trackerSampler) Sample(_ context.Context, limit int) []ids.NodeID {
	if limit <= 0 {
		return nil
	}
	nodeID, ok := s.peers.SelectPeer()
	if !ok {
		return nil
	}
	return []ids.NodeID{nodeID}
}

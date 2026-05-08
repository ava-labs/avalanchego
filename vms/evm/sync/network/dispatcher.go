// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package network provides Dispatcher (a typed synchronous client over
// one p2p handler ID) and aliases for the EVM state-sync RPCs.
package network

import (
	"context"
	"errors"
	"fmt"
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
	errUnmarshalResponse = errors.New("unmarshal response")
	errMarshalRequest    = errors.New("marshal request")
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
// nodeID. Selection is explicit (not AppRequestAny) so RegisterRequest
// runs before sending and prevents concurrent picks of the same peer.
func (d *Dispatcher[Req, Resp]) Send(ctx context.Context, req Req, resp Resp) (ids.NodeID, error) {
	nodeID, ok := d.peers.SelectPeer()
	if !ok {
		return ids.EmptyNodeID, errNoPeers
	}
	if err := d.SendTo(ctx, nodeID, req, resp); err != nil {
		return ids.EmptyNodeID, err
	}
	return nodeID, nil
}

// SendTo sends req to an explicit peer. The tracker is still notified
// for bandwidth scoring.
func (d *Dispatcher[Req, Resp]) SendTo(ctx context.Context, nodeID ids.NodeID, req Req, resp Resp) error {
	requestBytes, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("%w: %w", errMarshalRequest, err)
	}
	return d.dispatch(ctx, nodeID, requestBytes, resp)
}

// dispatch is the inner send-and-await cycle.
func (d *Dispatcher[Req, Resp]) dispatch(ctx context.Context, nodeID ids.NodeID, requestBytes []byte, resp Resp) error {
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
		return fmt.Errorf("%w: %w", errSendRequest, err)
	}

	select {
	case <-ctx.Done():
		d.peers.RegisterFailure(nodeID)
		return ctx.Err()
	case r := <-resultCh:
		if r.err != nil {
			d.peers.RegisterFailure(nodeID)
			return fmt.Errorf("%w: %w", errHandlerFailed, r.err)
		}
		if err := proto.Unmarshal(r.bytes, resp); err != nil {
			d.peers.RegisterFailure(nodeID)
			return fmt.Errorf("%w: %w", errUnmarshalResponse, err)
		}
		bandwidth := float64(len(r.bytes)) / (time.Since(start).Seconds() + epsilon)
		d.peers.RegisterResponse(nodeID, bandwidth)
		return nil
	}
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

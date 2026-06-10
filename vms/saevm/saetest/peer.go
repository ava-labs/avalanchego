// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var _ Peer = (*CapturingPeer)(nil)

// CapturingPeer is a [Peer] that captures AppResponse, AppRequestFailed, and
// AppGossip messages and exposes them for inspection. It answers inbound
// AppRequests with [p2p.ErrUnexpected].
type CapturingPeer struct {
	id       ids.NodeID
	sender   *Sender
	response *buffer.UnboundedBlockingDeque[peerResponse]
	gossip   *buffer.UnboundedBlockingDeque[peerGossip]
}

type peerResponse struct {
	nodeID    ids.NodeID
	requestID uint32
	bytes     []byte
	err       *common.AppError
}

type peerGossip struct {
	nodeID ids.NodeID
	bytes  []byte
}

// NewCapturingPeer returns a [CapturingPeer] with a fresh NodeID. vdrs is
// passed through to its underlying [Sender].
func NewCapturingPeer(tb testing.TB, vdrs set.Set[ids.NodeID]) *CapturingPeer {
	p := &CapturingPeer{
		id:       ids.GenerateTestNodeID(),
		sender:   NewSender(tb, vdrs),
		response: buffer.NewUnboundedBlockingDeque[peerResponse](1),
		gossip:   buffer.NewUnboundedBlockingDeque[peerGossip](1),
	}
	p.sender.SetSelf(p)
	return p
}

// Response blocks until the peer captures an AppResponse or AppRequestFailed
// to return.
func (p *CapturingPeer) Response() (ids.NodeID, uint32, []byte, *common.AppError) {
	r, _ := p.response.PopLeft()
	return r.nodeID, r.requestID, r.bytes, r.err
}

// Gossip blocks until the peer captures AppGossip to return.
func (p *CapturingPeer) Gossip() (ids.NodeID, []byte) {
	r, _ := p.gossip.PopLeft()
	return r.nodeID, r.bytes
}

func (p *CapturingPeer) NodeID() ids.NodeID { return p.id }
func (p *CapturingPeer) Sender() *Sender    { return p.sender }

func (p *CapturingPeer) AppResponse(_ context.Context, from ids.NodeID, requestID uint32, b []byte) error {
	p.response.PushRight(peerResponse{
		nodeID:    from,
		requestID: requestID,
		bytes:     b,
	})
	return nil
}

func (p *CapturingPeer) AppRequestFailed(_ context.Context, from ids.NodeID, requestID uint32, appErr *common.AppError) error {
	p.response.PushRight(peerResponse{
		nodeID:    from,
		requestID: requestID,
		err:       appErr,
	})
	return nil
}

func (p *CapturingPeer) AppGossip(_ context.Context, nodeID ids.NodeID, b []byte) error {
	p.gossip.PushRight(peerGossip{
		nodeID: nodeID,
		bytes:  b,
	})
	return nil
}

func (p *CapturingPeer) AppRequest(ctx context.Context, from ids.NodeID, requestID uint32, _ time.Time, _ []byte) error {
	return p.sender.SendAppError(
		ctx,
		from,
		requestID,
		p2p.ErrUnexpected.Code,
		p2p.ErrUnexpected.Message,
	)
}

func (*CapturingPeer) Connected(context.Context, ids.NodeID, *version.Application) error { return nil }
func (*CapturingPeer) Disconnected(context.Context, ids.NodeID) error                    { return nil }

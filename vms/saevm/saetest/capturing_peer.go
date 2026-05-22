// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

// CapturingPeer is a [Peer] that captures AppResponse and AppRequestFailed
// callbacks so tests can drive [common.AppHandler.AppRequest] against a local
// node and assert on what the node sent back.
//
// The Receive method returns the response bytes and any error in a single
// shot; exactly one of the two is non-nil per request.
type CapturingPeer struct {
	tb       testing.TB
	id       ids.NodeID
	sender   *Sender
	response chan capturedResponse
}

type capturedResponse struct {
	bytes []byte
	err   *common.AppError
}

// NewCapturingPeer returns a [CapturingPeer] with a fresh NodeID. vdrs is
// passed through to its underlying [Sender], matching the validator set the
// peer should consider when sampling for gossip.
func NewCapturingPeer(tb testing.TB, vdrs set.Set[ids.NodeID]) *CapturingPeer {
	p := &CapturingPeer{
		tb:       tb,
		id:       ids.GenerateTestNodeID(),
		sender:   NewSender(tb, vdrs),
		response: make(chan capturedResponse, 1),
	}
	p.sender.SetSelf(p)
	return p
}

// NodeID returns the peer's NodeID.
func (p *CapturingPeer) NodeID() ids.NodeID { return p.id }

// Sender returns the peer's [Sender].
func (p *CapturingPeer) Sender() *Sender { return p.sender }

// Receive blocks until the peer captures a single AppResponse or
// AppRequestFailed callback and returns the result. Exactly one of bytes and
// appErr is non-nil. Receive fails the test if ctx is cancelled first.
func (p *CapturingPeer) Receive(ctx context.Context) ([]byte, *common.AppError) {
	p.tb.Helper()

	select {
	case r := <-p.response:
		return r.bytes, r.err
	case <-ctx.Done():
		require.FailNowf(p.tb, "context cancelled while waiting for capture", "%s", ctx.Err())
		return nil, nil
	}
}

// AppResponse captures the response bytes.
func (p *CapturingPeer) AppResponse(_ context.Context, _ ids.NodeID, _ uint32, b []byte) error {
	p.response <- capturedResponse{bytes: b}
	return nil
}

// AppRequestFailed captures the error.
func (p *CapturingPeer) AppRequestFailed(_ context.Context, _ ids.NodeID, _ uint32, appErr *common.AppError) error {
	p.response <- capturedResponse{err: appErr}
	return nil
}

// AppGossip is a no-op; gossip is not part of the capture surface.
func (*CapturingPeer) AppGossip(context.Context, ids.NodeID, []byte) error { return nil }

// AppRequest fails the test if invoked; this peer is only meant to receive
// responses, not requests.
func (p *CapturingPeer) AppRequest(_ context.Context, from ids.NodeID, _ uint32, _ time.Time, _ []byte) error {
	p.tb.Helper()
	require.FailNowf(p.tb, "unexpected AppRequest on CapturingPeer", "from %s", from)
	return nil
}

// Connected is a no-op.
func (*CapturingPeer) Connected(context.Context, ids.NodeID, *version.Application) error {
	return nil
}

// Disconnected is a no-op.
func (*CapturingPeer) Disconnected(context.Context, ids.NodeID) error { return nil }

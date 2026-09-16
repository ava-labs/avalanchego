// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
)

var (
	errSendRequest       = errors.New("send request")
	errHandlerFailed     = errors.New("handler request failed")
	errMarshalRequest    = errors.New("marshal request")
	errUnmarshalResponse = errors.New("unmarshal response")
)

// Dispatcher is a typed synchronous client bound to one handler ID.
// Use one instance per RPC type.
type Dispatcher[Req, Resp proto.Message] struct {
	client *p2p.TrackingClient
}

// NewDispatcher returns a [Dispatcher] bound to handlerID on n, selecting and
// scoring peers with peers.
func NewDispatcher[Req, Resp proto.Message](
	n *p2p.Network,
	handlerID uint64,
	peers *p2p.PeerTracker,
) *Dispatcher[Req, Resp] {
	return &Dispatcher[Req, Resp]{
		client: n.NewTrackingClient(handlerID, peers),
	}
}

// Send issues req to a bandwidth-chosen peer and decodes the reply into resp.
// A non-nil error from validate de-scores that peer, like a transport failure.
//
// A reply arriving after ctx ends still writes resp, so give each call its own.
func (d *Dispatcher[Req, Resp]) Send(
	ctx context.Context,
	req Req,
	resp Resp,
	validate func(nodeID ids.NodeID, resp Resp) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	requestBytes, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("%w: %w", errMarshalRequest, err)
	}

	// Closed before validation starts, so that a cancelled caller can tell a
	// reply being validated from one that never came.
	arrived := make(chan struct{})
	// Buffered so a reply landing before awaitReply selects never blocks the
	// handler goroutine.
	result := make(chan error, 1)
	onResponse := func(_ context.Context, nodeID ids.NodeID, responseBytes []byte, appErr error) error {
		close(arrived)
		verdict := decode(nodeID, responseBytes, appErr, resp, validate)
		result <- verdict
		return verdict
	}

	if err := d.client.AppRequestAny(ctx, requestBytes, onResponse); err != nil {
		return fmt.Errorf("%w: %w", errSendRequest, err)
	}
	return awaitReply(ctx, arrived, result)
}

// awaitReply returns the validated reply, or ctx's error if none arrived. A
// reply already being validated is waited for rather than discarded.
func awaitReply(ctx context.Context, arrived <-chan struct{}, result <-chan error) error {
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		select {
		case <-arrived:
			return <-result
		default:
			return ctx.Err()
		}
	}
}

func decode[Resp proto.Message](
	nodeID ids.NodeID,
	responseBytes []byte,
	appErr error,
	resp Resp,
	validate func(nodeID ids.NodeID, resp Resp) error,
) error {
	if appErr != nil {
		return fmt.Errorf("%w: %w", errHandlerFailed, appErr)
	}
	if err := proto.Unmarshal(responseBytes, resp); err != nil {
		return fmt.Errorf("%w: %w", errUnmarshalResponse, err)
	}
	return validate(nodeID, resp)
}

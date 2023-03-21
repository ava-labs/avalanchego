// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/components/message"
	"go.uber.org/zap"
)

var _ common.AppHandler = (*network)(nil)

type network struct {
	ctx *snow.Context

	gossipHandler message.Handler
}

func NewNetwork(
	ctx *snow.Context,
	gossipHandler message.Handler,
) common.AppHandler {
	return &network{
		ctx:           ctx,
		gossipHandler: gossipHandler,
	}
}

func (*network) CrossChainAppRequestFailed(context.Context, ids.ID, uint32) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (*network) CrossChainAppRequest(context.Context, ids.ID, uint32, time.Time, []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (*network) CrossChainAppResponse(context.Context, ids.ID, uint32, []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (*network) AppRequestFailed(context.Context, ids.NodeID, uint32) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (*network) AppRequest(context.Context, ids.NodeID, uint32, time.Time, []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (*network) AppResponse(context.Context, ids.NodeID, uint32, []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (n *network) AppGossip(_ context.Context, nodeID ids.NodeID, msgBytes []byte) error {
	n.ctx.Log.Debug("called AppGossip message handler",
		zap.Stringer("nodeID", nodeID),
		zap.Int("messageLen", len(msgBytes)),
	)
	msg, err := message.Parse(msgBytes)
	if err != nil {
		n.ctx.Log.Debug("dropping AppGossip message",
			zap.String("reason", "failed to parse message"),
			zap.Error(err),
		)
		return nil
	}

	// gossip messages does not use requestID
	// TODO: use HandleGossip and omit requestID?
	return msg.Handle(n.gossipHandler, nodeID, 0)
}

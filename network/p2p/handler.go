// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// Handler is the server-side logic for virtual machine application protocols.
type Handler interface {
	// AppGossip is called when handling an AppGossip message.
	AppGossip(
		ctx context.Context,
		nodeID ids.NodeID,
		gossipBytes []byte,
	) error
	// AppRequest is called when handling an AppRequest message.
	// Returns the bytes for the response corresponding to [requestBytes]
	AppRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		deadline time.Time,
		requestBytes []byte,
	) ([]byte, error)
	// CrossChainAppRequest is called when handling a CrossChainAppRequest
	// message.
	// Returns the bytes for the response corresponding to [requestBytes]
	CrossChainAppRequest(
		ctx context.Context,
		chainID ids.ID,
		deadline time.Time,
		requestBytes []byte,
	) ([]byte, error)
}

// responder automatically sends the response for a given request
type responder struct {
	handlerID uint64
	handler   Handler
	log       logging.Logger
	sender    common.AppSender
}

func (r *responder) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	appResponse, err := r.handler.AppRequest(ctx, nodeID, deadline, request)
	if err != nil {
		r.log.Debug("failed to handle message",
			zap.Stringer("messageOp", message.AppRequestOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Time("deadline", deadline),
			zap.Uint64("handlerID", r.handlerID),
			zap.Binary("message", request),
		)
		return nil
	}

	return r.sender.SendAppResponse(ctx, nodeID, requestID, appResponse)
}

func (r *responder) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	err := r.handler.AppGossip(ctx, nodeID, msg)
	if err != nil {
		r.log.Debug("failed to handle message",
			zap.Stringer("messageOp", message.AppGossipOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint64("handlerID", r.handlerID),
			zap.Binary("message", msg),
		)
	}
	return nil
}

func (r *responder) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	appResponse, err := r.handler.CrossChainAppRequest(ctx, chainID, deadline, request)
	if err != nil {
		r.log.Debug("failed to handle message",
			zap.Stringer("messageOp", message.CrossChainAppRequestOp),
			zap.Stringer("chainID", chainID),
			zap.Uint32("requestID", requestID),
			zap.Time("deadline", deadline),
			zap.Uint64("handlerID", r.handlerID),
			zap.Binary("message", request),
		)
		return nil
	}

	return r.sender.SendCrossChainAppResponse(ctx, chainID, requestID, appResponse)
}

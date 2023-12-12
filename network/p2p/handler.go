// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	ErrNotValidator = errors.New("not a validator")

	_ Handler = (*NoOpHandler)(nil)
	_ Handler = (*ValidatorHandler)(nil)
)

// Handler is the server-side logic for virtual machine application protocols.
type Handler interface {
	// AppGossip is called when handling an AppGossip message.
	AppGossip(
		ctx context.Context,
		nodeID ids.NodeID,
		gossipBytes []byte,
	)
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

// NoOpHandler drops all messages
type NoOpHandler struct{}

func (NoOpHandler) AppGossip(context.Context, ids.NodeID, []byte) {}

func (NoOpHandler) AppRequest(context.Context, ids.NodeID, time.Time, []byte) ([]byte, error) {
	return nil, nil
}

func (NoOpHandler) CrossChainAppRequest(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
	return nil, nil
}

// ValidatorHandler drops messages from non-validators
type ValidatorHandler struct {
	Handler
	ValidatorSet ValidatorSet
	Log          logging.Logger
}

func (v ValidatorHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if !v.ValidatorSet.Has(ctx, nodeID) {
		v.Log.Debug("dropping message", zap.Stringer("nodeID", nodeID))
		return
	}

	v.Handler.AppGossip(ctx, nodeID, gossipBytes)
}

func (v ValidatorHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	if !v.ValidatorSet.Has(ctx, nodeID) {
		return nil, ErrNotValidator
	}

	return v.Handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

// responder automatically sends the response for a given request
type responder struct {
	Handler
	handlerID uint64
	log       logging.Logger
	sender    common.AppSender
}

// AppRequest calls the underlying handler and sends back the response to nodeID
func (r *responder) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	appResponse, err := r.Handler.AppRequest(ctx, nodeID, deadline, request)
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

// CrossChainAppRequest calls the underlying handler and sends back the response
// to chainID
func (r *responder) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	appResponse, err := r.Handler.CrossChainAppRequest(ctx, chainID, deadline, request)
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

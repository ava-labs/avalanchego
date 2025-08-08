// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"math"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// Standardized identifiers for application protocol handlers
const (
	TxGossipHandlerID = iota
	AtomicTxGossipHandlerID
	// SignatureRequestHandlerID is specified in ACP-118: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/118-warp-signature-request
	SignatureRequestHandlerID
)

var (
	_ Handler = (*NoOpHandler)(nil)
	_ Handler = (*TestHandler)(nil)
	_ Handler = (*ValidatorHandler)(nil)
	_ Handler = (*throttleValidatorAppRequestHandler)(nil)
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
	// Sends a response with the response corresponding to [requestBytes] or
	// an application-defined error.
	AppRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		deadline time.Time,
		requestBytes []byte,
	) ([]byte, *common.AppError)
}

// NoOpHandler drops all messages
type NoOpHandler struct{}

func (NoOpHandler) AppGossip(context.Context, ids.NodeID, []byte) {}

func (NoOpHandler) AppRequest(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
	return nil, nil
}

type throttleValidatorAppRequestHandler struct {
	appGossipHandler  Handler
	appRequestHandler Handler
}

func (t throttleValidatorAppRequestHandler) AppGossip(
	ctx context.Context,
	nodeID ids.NodeID,
	gossipBytes []byte,
) {
	t.appGossipHandler.AppGossip(ctx, nodeID, gossipBytes)
}

func (t throttleValidatorAppRequestHandler) AppRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	deadline time.Time,
	requestBytes []byte,
) ([]byte, *common.AppError) {
	return t.appRequestHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

type dynamicThrottlerHandler struct {
	Handler         *ThrottlerHandler
	Throttler       *SlidingWindowThrottler
	ValidatorSet    ValidatorSet
	RequestsPerPeer float64

	prevNumConnectedValidators int
}

func (d *dynamicThrottlerHandler) AppGossip(
	ctx context.Context,
	nodeID ids.NodeID,
	gossipBytes []byte,
) {
	d.checkUpdateThrottlingLimit(ctx)

	d.Handler.AppGossip(ctx, nodeID, gossipBytes)
}

func (d *dynamicThrottlerHandler) AppRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	deadline time.Time,
	requestBytes []byte,
) ([]byte, *common.AppError) {
	d.checkUpdateThrottlingLimit(ctx)

	return d.Handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

func (d *dynamicThrottlerHandler) checkUpdateThrottlingLimit(ctx context.Context) {
	numValidators := d.ValidatorSet.Len(ctx)

	if numValidators == d.prevNumConnectedValidators {
		return
	}

	d.prevNumConnectedValidators = numValidators

	n := float64(numValidators)

	// guaranteed to not overflow an int
	expectedSamples := d.RequestsPerPeer / n
	variance := d.RequestsPerPeer * (n - 1) / (n * n)
	stdDeviation := math.Sqrt(variance)

	limit := int(expectedSamples + 4*stdDeviation)
	d.Throttler.setLimit(limit)
}

// NewHandler wraps a handler with defaults.
// Period is the throttling evaluation period during which this node is
// expecting each peer to make requestsPerPeer requests to the network. The
// throttling limit is dynamically updated to be inversely proportional to the
// number of connected network validators.
func NewHandler(
	log logging.Logger,
	handler Handler,
	validatorSet ValidatorSet,
	period time.Duration,
	requestsPerPeer int,
) Handler {
	// Throttling limit will be initialized when a request is handled
	throttler := NewSlidingWindowThrottler(period, 0)

	throttlerHandler := &dynamicThrottlerHandler{
		Handler:         NewThrottlerHandler(handler, throttler, log),
		Throttler:       throttler,
		ValidatorSet:    validatorSet,
		RequestsPerPeer: float64(requestsPerPeer),
	}

	return throttleValidatorAppRequestHandler{
		appGossipHandler:  handler,
		appRequestHandler: NewValidatorHandler(throttlerHandler, validatorSet, log),
	}
}

func NewValidatorHandler(
	handler Handler,
	validatorSet ValidatorSet,
	log logging.Logger,
) *ValidatorHandler {
	return &ValidatorHandler{
		handler:      handler,
		validatorSet: validatorSet,
		log:          log,
	}
}

// ValidatorHandler drops messages from non-validators
type ValidatorHandler struct {
	handler      Handler
	validatorSet ValidatorSet
	log          logging.Logger
}

func (v ValidatorHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if !v.validatorSet.Has(ctx, nodeID) {
		v.log.Debug("dropping message",
			zap.Stringer("nodeID", nodeID),
			zap.String("reason", "not a validator"),
		)
		return
	}

	v.handler.AppGossip(ctx, nodeID, gossipBytes)
}

func (v ValidatorHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	if !v.validatorSet.Has(ctx, nodeID) {
		return nil, ErrNotValidator
	}

	return v.handler.AppRequest(ctx, nodeID, deadline, requestBytes)
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
			zap.Error(err),
		)
		return r.sender.SendAppError(ctx, nodeID, requestID, err.Code, err.Message)
	}

	return r.sender.SendAppResponse(ctx, nodeID, requestID, appResponse)
}

type TestHandler struct {
	AppGossipF  func(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte)
	AppRequestF func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError)
}

func (t TestHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if t.AppGossipF == nil {
		return
	}

	t.AppGossipF(ctx, nodeID, gossipBytes)
}

func (t TestHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	if t.AppRequestF == nil {
		return nil, nil
	}

	return t.AppRequestF(ctx, nodeID, deadline, requestBytes)
}

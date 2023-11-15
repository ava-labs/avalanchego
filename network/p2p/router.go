// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
)

var (
	ErrExistingAppProtocol = errors.New("existing app protocol")
	ErrUnrequestedResponse = errors.New("unrequested response")

	_ common.AppHandler = (*Router)(nil)
)

type metrics struct {
	appRequestTime                 metric.Averager
	appRequestFailedTime           metric.Averager
	appResponseTime                metric.Averager
	appGossipTime                  metric.Averager
	crossChainAppRequestTime       metric.Averager
	crossChainAppRequestFailedTime metric.Averager
	crossChainAppResponseTime      metric.Averager
}

type pendingAppRequest struct {
	*metrics
	AppResponseCallback
}

type pendingCrossChainAppRequest struct {
	*metrics
	CrossChainAppResponseCallback
}

// meteredHandler emits metrics for a Handler
type meteredHandler struct {
	*responder
	*metrics
}

// Router routes incoming application messages to the corresponding registered
// app handler. App messages must be made using the registered handler's
// corresponding Client.
type Router struct {
	log       logging.Logger
	sender    common.AppSender
	metrics   prometheus.Registerer
	namespace string

	lock                         sync.RWMutex
	handlers                     map[uint64]*meteredHandler
	pendingAppRequests           map[uint32]pendingAppRequest
	pendingCrossChainAppRequests map[uint32]pendingCrossChainAppRequest
	requestID                    uint32
}

// NewRouter returns a new instance of Router
func NewRouter(
	log logging.Logger,
	sender common.AppSender,
	metrics prometheus.Registerer,
	namespace string,
) *Router {
	return &Router{
		log:                          log,
		sender:                       sender,
		metrics:                      metrics,
		namespace:                    namespace,
		handlers:                     make(map[uint64]*meteredHandler),
		pendingAppRequests:           make(map[uint32]pendingAppRequest),
		pendingCrossChainAppRequests: make(map[uint32]pendingCrossChainAppRequest),
		// invariant: sdk uses odd-numbered requestIDs
		requestID: 1,
	}
}

// RegisterAppProtocol reserves an identifier for an application protocol and
// returns a Client that can be used to send messages for the corresponding
// protocol.
func (r *Router) RegisterAppProtocol(handlerID uint64, handler Handler, nodeSampler NodeSampler) (*Client, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	if _, ok := r.handlers[handlerID]; ok {
		return nil, fmt.Errorf("failed to register handler id %d: %w", handlerID, ErrExistingAppProtocol)
	}

	appRequestTime, err := metric.NewAverager(
		r.namespace,
		fmt.Sprintf("handler_%d_app_request", handlerID),
		"app request time (ns)",
		r.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register app request metric for handler_%d: %w", handlerID, err)
	}

	appRequestFailedTime, err := metric.NewAverager(
		r.namespace,
		fmt.Sprintf("handler_%d_app_request_failed", handlerID),
		"app request failed time (ns)",
		r.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register app request failed metric for handler_%d: %w", handlerID, err)
	}

	appResponseTime, err := metric.NewAverager(
		r.namespace,
		fmt.Sprintf("handler_%d_app_response", handlerID),
		"app response time (ns)",
		r.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register app response metric for handler_%d: %w", handlerID, err)
	}

	appGossipTime, err := metric.NewAverager(
		r.namespace,
		fmt.Sprintf("handler_%d_app_gossip", handlerID),
		"app gossip time (ns)",
		r.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register app gossip metric for handler_%d: %w", handlerID, err)
	}

	crossChainAppRequestTime, err := metric.NewAverager(
		r.namespace,
		fmt.Sprintf("handler_%d_cross_chain_app_request", handlerID),
		"cross chain app request time (ns)",
		r.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register cross-chain app request metric for handler_%d: %w", handlerID, err)
	}

	crossChainAppRequestFailedTime, err := metric.NewAverager(
		r.namespace,
		fmt.Sprintf("handler_%d_cross_chain_app_request_failed", handlerID),
		"app request failed time (ns)",
		r.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register cross-chain app request failed metric for handler_%d: %w", handlerID, err)
	}

	crossChainAppResponseTime, err := metric.NewAverager(
		r.namespace,
		fmt.Sprintf("handler_%d_cross_chain_app_response", handlerID),
		"cross chain app response time (ns)",
		r.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register cross-chain app response metric for handler_%d: %w", handlerID, err)
	}

	r.handlers[handlerID] = &meteredHandler{
		responder: &responder{
			Handler:   handler,
			handlerID: handlerID,
			log:       r.log,
			sender:    r.sender,
		},
		metrics: &metrics{
			appRequestTime:                 appRequestTime,
			appRequestFailedTime:           appRequestFailedTime,
			appResponseTime:                appResponseTime,
			appGossipTime:                  appGossipTime,
			crossChainAppRequestTime:       crossChainAppRequestTime,
			crossChainAppRequestFailedTime: crossChainAppRequestFailedTime,
			crossChainAppResponseTime:      crossChainAppResponseTime,
		},
	}

	return &Client{
		handlerID:     handlerID,
		handlerPrefix: binary.AppendUvarint(nil, handlerID),
		sender:        r.sender,
		router:        r,
		nodeSampler:   nodeSampler,
	}, nil
}

// AppRequest routes an AppRequest to a Handler based on the handler prefix. The
// message is dropped if no matching handler can be found.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *Router) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	start := time.Now()
	parsedMsg, handler, ok := r.parse(request)
	if !ok {
		r.log.Debug("failed to process message",
			zap.Stringer("messageOp", message.AppRequestOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Time("deadline", deadline),
			zap.Binary("message", request),
		)
		return nil
	}

	// call the corresponding handler and send back a response to nodeID
	if err := handler.AppRequest(ctx, nodeID, requestID, deadline, parsedMsg); err != nil {
		return err
	}

	handler.metrics.appRequestTime.Observe(float64(time.Since(start)))
	return nil
}

// AppRequestFailed routes an AppRequestFailed message to the callback
// corresponding to requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *Router) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, err *common.AppError) error {
	start := time.Now()
	pending, ok := r.clearAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.AppResponseCallback(ctx, nodeID, nil, err)
	pending.appRequestFailedTime.Observe(float64(time.Since(start)))
	return nil
}

// AppResponse routes an AppResponse message to the callback corresponding to
// requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *Router) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	start := time.Now()
	pending, ok := r.clearAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.AppResponseCallback(ctx, nodeID, response, nil)
	pending.appResponseTime.Observe(float64(time.Since(start)))
	return nil
}

// AppGossip routes an AppGossip message to a Handler based on the handler
// prefix. The message is dropped if no matching handler can be found.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *Router) AppGossip(ctx context.Context, nodeID ids.NodeID, gossip []byte) error {
	start := time.Now()
	parsedMsg, handler, ok := r.parse(gossip)
	if !ok {
		r.log.Debug("failed to process message",
			zap.Stringer("messageOp", message.AppGossipOp),
			zap.Stringer("nodeID", nodeID),
			zap.Binary("message", gossip),
		)
		return nil
	}

	handler.AppGossip(ctx, nodeID, parsedMsg)

	handler.metrics.appGossipTime.Observe(float64(time.Since(start)))
	return nil
}

// CrossChainAppRequest routes a CrossChainAppRequest message to a Handler
// based on the handler prefix. The message is dropped if no matching handler
// can be found.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *Router) CrossChainAppRequest(
	ctx context.Context,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	msg []byte,
) error {
	start := time.Now()
	parsedMsg, handler, ok := r.parse(msg)
	if !ok {
		r.log.Debug("failed to process message",
			zap.Stringer("messageOp", message.CrossChainAppRequestOp),
			zap.Stringer("chainID", chainID),
			zap.Uint32("requestID", requestID),
			zap.Time("deadline", deadline),
			zap.Binary("message", msg),
		)
		return nil
	}

	if err := handler.CrossChainAppRequest(ctx, chainID, requestID, deadline, parsedMsg); err != nil {
		return err
	}

	handler.metrics.crossChainAppRequestTime.Observe(float64(time.Since(start)))
	return nil
}

// CrossChainAppRequestFailed routes a CrossChainAppRequestFailed message to
// the callback corresponding to requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *Router) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32, err *common.AppError) error {
	start := time.Now()
	pending, ok := r.clearCrossChainAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.CrossChainAppResponseCallback(ctx, chainID, nil, err)
	pending.crossChainAppRequestFailedTime.Observe(float64(time.Since(start)))
	return nil
}

// CrossChainAppResponse routes a CrossChainAppResponse message to the callback
// corresponding to requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *Router) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	start := time.Now()
	pending, ok := r.clearCrossChainAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.CrossChainAppResponseCallback(ctx, chainID, response, nil)
	pending.crossChainAppResponseTime.Observe(float64(time.Since(start)))
	return nil
}

// Parse parses a gossip or request message and maps it to a corresponding
// handler if present.
//
// Returns:
// - The unprefixed protocol message.
// - The protocol responder.
// - A boolean indicating that parsing succeeded.
//
// Invariant: Assumes [r.lock] isn't held.
func (r *Router) parse(msg []byte) ([]byte, *meteredHandler, bool) {
	handlerID, bytesRead := binary.Uvarint(msg)
	if bytesRead <= 0 {
		return nil, nil, false
	}

	r.lock.RLock()
	defer r.lock.RUnlock()

	handler, ok := r.handlers[handlerID]
	return msg[bytesRead:], handler, ok
}

// Invariant: Assumes [r.lock] isn't held.
func (r *Router) clearAppRequest(requestID uint32) (pendingAppRequest, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	callback, ok := r.pendingAppRequests[requestID]
	delete(r.pendingAppRequests, requestID)
	return callback, ok
}

// Invariant: Assumes [r.lock] isn't held.
func (r *Router) clearCrossChainAppRequest(requestID uint32) (pendingCrossChainAppRequest, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	callback, ok := r.pendingCrossChainAppRequests[requestID]
	delete(r.pendingCrossChainAppRequests, requestID)
	return callback, ok
}

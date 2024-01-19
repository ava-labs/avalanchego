// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	ErrExistingAppProtocol = errors.New("existing app protocol")
	ErrUnrequestedResponse = errors.New("unrequested response")

	_ common.AppHandler = (*router)(nil)
)

type pendingAppRequest struct {
	handlerID string
	callback  AppResponseCallback
}

type pendingCrossChainAppRequest struct {
	handlerID string
	callback  CrossChainAppResponseCallback
}

// meteredHandler emits metrics for a Handler
type meteredHandler struct {
	*responder
	metrics
}

type metrics struct {
	appRequestTime                  *prometheus.CounterVec
	appRequestCount                 *prometheus.CounterVec
	appResponseTime                 *prometheus.CounterVec
	appResponseCount                *prometheus.CounterVec
	appRequestFailedTime            *prometheus.CounterVec
	appRequestFailedCount           *prometheus.CounterVec
	appGossipTime                   *prometheus.CounterVec
	appGossipCount                  *prometheus.CounterVec
	crossChainAppRequestTime        *prometheus.CounterVec
	crossChainAppRequestCount       *prometheus.CounterVec
	crossChainAppResponseTime       *prometheus.CounterVec
	crossChainAppResponseCount      *prometheus.CounterVec
	crossChainAppRequestFailedTime  *prometheus.CounterVec
	crossChainAppRequestFailedCount *prometheus.CounterVec
}

// router routes incoming application messages to the corresponding registered
// app handler. App messages must be made using the registered handler's
// corresponding Client.
type router struct {
	log     logging.Logger
	sender  common.AppSender
	metrics metrics

	lock                         sync.RWMutex
	handlers                     map[uint64]*meteredHandler
	pendingAppRequests           map[uint32]pendingAppRequest
	pendingCrossChainAppRequests map[uint32]pendingCrossChainAppRequest
	requestID                    uint32
}

// newRouter returns a new instance of Router
func newRouter(
	log logging.Logger,
	sender common.AppSender,
	metrics metrics,
) *router {
	return &router{
		log:                          log,
		sender:                       sender,
		metrics:                      metrics,
		handlers:                     make(map[uint64]*meteredHandler),
		pendingAppRequests:           make(map[uint32]pendingAppRequest),
		pendingCrossChainAppRequests: make(map[uint32]pendingCrossChainAppRequest),
		// invariant: sdk uses odd-numbered requestIDs
		requestID: 1,
	}
}

func (r *router) addHandler(handlerID uint64, handler Handler) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	if _, ok := r.handlers[handlerID]; ok {
		return fmt.Errorf("failed to register handler id %d: %w", handlerID, ErrExistingAppProtocol)
	}

	r.handlers[handlerID] = &meteredHandler{
		responder: &responder{
			Handler:   handler,
			handlerID: handlerID,
			log:       r.log,
			sender:    r.sender,
		},
		metrics: r.metrics,
	}

	return nil
}

// AppRequest routes an AppRequest to a Handler based on the handler prefix. The
// message is dropped if no matching handler can be found.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	start := time.Now()
	parsedMsg, handler, handlerID, ok := r.parse(request)
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

	labels := prometheus.Labels{
		handlerLabel: handlerID,
	}

	metricCount, err := r.metrics.appRequestCount.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricTime, err := r.metrics.appRequestTime.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricCount.Inc()
	metricTime.Add(float64(time.Since(start)))

	return nil
}

// AppRequestFailed routes an AppRequestFailed message to the callback
// corresponding to requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	start := time.Now()
	pending, ok := r.clearAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.callback(ctx, nodeID, nil, appErr)

	labels := prometheus.Labels{
		handlerLabel: pending.handlerID,
	}

	metricCount, err := r.metrics.appRequestFailedCount.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricTime, err := r.metrics.appRequestFailedTime.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricCount.Inc()
	metricTime.Add(float64(time.Since(start)))

	return nil
}

// AppResponse routes an AppResponse message to the callback corresponding to
// requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	start := time.Now()
	pending, ok := r.clearAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.callback(ctx, nodeID, response, nil)

	labels := prometheus.Labels{
		handlerLabel: pending.handlerID,
	}

	metricCount, err := r.metrics.appResponseCount.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricTime, err := r.metrics.appResponseTime.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricCount.Inc()
	metricTime.Add(float64(time.Since(start)))

	return nil
}

// AppGossip routes an AppGossip message to a Handler based on the handler
// prefix. The message is dropped if no matching handler can be found.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) AppGossip(ctx context.Context, nodeID ids.NodeID, gossip []byte) error {
	start := time.Now()
	parsedMsg, handler, handlerID, ok := r.parse(gossip)
	if !ok {
		r.log.Debug("failed to process message",
			zap.Stringer("messageOp", message.AppGossipOp),
			zap.Stringer("nodeID", nodeID),
			zap.Binary("message", gossip),
		)
		return nil
	}

	handler.AppGossip(ctx, nodeID, parsedMsg)

	labels := prometheus.Labels{
		handlerLabel: handlerID,
	}

	metricCount, err := r.metrics.appGossipCount.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricTime, err := r.metrics.appGossipTime.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricCount.Inc()
	metricTime.Add(float64(time.Since(start)))

	return nil
}

// CrossChainAppRequest routes a CrossChainAppRequest message to a Handler
// based on the handler prefix. The message is dropped if no matching handler
// can be found.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) CrossChainAppRequest(
	ctx context.Context,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	msg []byte,
) error {
	start := time.Now()
	parsedMsg, handler, handlerID, ok := r.parse(msg)
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

	labels := prometheus.Labels{
		handlerLabel: handlerID,
	}

	metricCount, err := r.metrics.crossChainAppRequestCount.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricTime, err := r.metrics.crossChainAppRequestTime.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricCount.Inc()
	metricTime.Add(float64(time.Since(start)))

	return nil
}

// CrossChainAppRequestFailed routes a CrossChainAppRequestFailed message to
// the callback corresponding to requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32, appErr *common.AppError) error {
	start := time.Now()
	pending, ok := r.clearCrossChainAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.callback(ctx, chainID, nil, appErr)

	labels := prometheus.Labels{
		handlerLabel: pending.handlerID,
	}

	metricCount, err := r.metrics.crossChainAppRequestFailedCount.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricTime, err := r.metrics.crossChainAppRequestFailedTime.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricCount.Inc()
	metricTime.Add(float64(time.Since(start)))

	return nil
}

// CrossChainAppResponse routes a CrossChainAppResponse message to the callback
// corresponding to requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	start := time.Now()
	pending, ok := r.clearCrossChainAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.callback(ctx, chainID, response, nil)

	labels := prometheus.Labels{
		handlerLabel: pending.handlerID,
	}

	metricCount, err := r.metrics.crossChainAppResponseCount.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricTime, err := r.metrics.crossChainAppResponseTime.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricCount.Inc()
	metricTime.Add(float64(time.Since(start)))

	return nil
}

// Parse parses a gossip or request message and maps it to a corresponding
// handler if present.
//
// Returns:
// - The unprefixed protocol message.
// - The protocol responder.
// - The protocol metric name.
// - A boolean indicating that parsing succeeded.
//
// Invariant: Assumes [r.lock] isn't held.
func (r *router) parse(msg []byte) ([]byte, *meteredHandler, string, bool) {
	handlerID, bytesRead := binary.Uvarint(msg)
	if bytesRead <= 0 {
		return nil, nil, "", false
	}

	r.lock.RLock()
	defer r.lock.RUnlock()

	handlerStr := strconv.FormatUint(handlerID, 10)
	handler, ok := r.handlers[handlerID]
	return msg[bytesRead:], handler, handlerStr, ok
}

// Invariant: Assumes [r.lock] isn't held.
func (r *router) clearAppRequest(requestID uint32) (pendingAppRequest, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	callback, ok := r.pendingAppRequests[requestID]
	delete(r.pendingAppRequests, requestID)
	return callback, ok
}

// Invariant: Assumes [r.lock] isn't held.
func (r *router) clearCrossChainAppRequest(requestID uint32) (pendingCrossChainAppRequest, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	callback, ok := r.pendingCrossChainAppRequests[requestID]
	delete(r.pendingCrossChainAppRequests, requestID)
	return callback, ok
}

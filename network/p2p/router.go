// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

type metrics struct {
	msgTime  *prometheus.GaugeVec
	msgCount *prometheus.CounterVec
}

func (m *metrics) observe(labels prometheus.Labels, start time.Time) error {
	metricTime, err := m.msgTime.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricCount, err := m.msgCount.GetMetricWith(labels)
	if err != nil {
		return err
	}

	metricTime.Add(float64(time.Since(start)))
	metricCount.Inc()
	return nil
}

// router routes incoming application messages to the corresponding registered
// app handler. App messages must be made using the registered handler's
// corresponding Client.
type router struct {
	log     logging.Logger
	sender  common.AppSender
	metrics metrics

	lock               sync.RWMutex
	handlers           map[uint64]*responder
	pendingAppRequests map[uint32]pendingAppRequest
	requestID          uint32
}

// newRouter returns a new instance of Router
func newRouter(
	log logging.Logger,
	sender common.AppSender,
	metrics metrics,
) *router {
	return &router{
		log:                log,
		sender:             sender,
		metrics:            metrics,
		handlers:           make(map[uint64]*responder),
		pendingAppRequests: make(map[uint32]pendingAppRequest),
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

	r.handlers[handlerID] = &responder{
		Handler:   handler,
		handlerID: handlerID,
		log:       r.log,
		sender:    r.sender,
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
		r.log.Debug("received message for unregistered handler",
			zap.Stringer("messageOp", message.AppRequestOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Time("deadline", deadline),
			zap.Binary("message", request),
		)

		// Send an error back to the requesting peer. Invalid requests that we
		// cannot parse a handler id for are handled the same way as requests
		// for which we do not have a registered handler.
		return r.sender.SendAppError(ctx, nodeID, requestID, ErrUnregisteredHandler.Code, ErrUnregisteredHandler.Message)
	}

	// call the corresponding handler and send back a response to nodeID
	if err := handler.AppRequest(ctx, nodeID, requestID, deadline, parsedMsg); err != nil {
		return err
	}

	return r.metrics.observe(
		prometheus.Labels{
			opLabel:      message.AppRequestOp.String(),
			handlerLabel: handlerID,
		},
		start,
	)
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

	return r.metrics.observe(
		prometheus.Labels{
			opLabel:      message.AppErrorOp.String(),
			handlerLabel: pending.handlerID,
		},
		start,
	)
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

	return r.metrics.observe(
		prometheus.Labels{
			opLabel:      message.AppResponseOp.String(),
			handlerLabel: pending.handlerID,
		},
		start,
	)
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
		r.log.Debug("received message for unregistered handler",
			zap.Stringer("messageOp", message.AppGossipOp),
			zap.Stringer("nodeID", nodeID),
			zap.Binary("message", gossip),
		)
		return nil
	}

	handler.AppGossip(ctx, nodeID, parsedMsg)

	return r.metrics.observe(
		prometheus.Labels{
			opLabel:      message.AppGossipOp.String(),
			handlerLabel: handlerID,
		},
		start,
	)
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
func (r *router) parse(prefixedMsg []byte) ([]byte, *responder, string, bool) {
	handlerID, msg, ok := ParseMessage(prefixedMsg)
	if !ok {
		return nil, nil, "", false
	}

	handlerStr := strconv.FormatUint(handlerID, 10)

	r.lock.RLock()
	defer r.lock.RUnlock()

	handler, ok := r.handlers[handlerID]
	return msg, handler, handlerStr, ok
}

// Invariant: Assumes [r.lock] isn't held.
func (r *router) clearAppRequest(requestID uint32) (pendingAppRequest, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	callback, ok := r.pendingAppRequests[requestID]
	delete(r.pendingAppRequests, requestID)
	return callback, ok
}

// Parse a gossip or request message.
//
// Returns:
// - The protocol ID.
// - The unprefixed protocol message.
// - A boolean indicating that parsing succeeded.
func ParseMessage(msg []byte) (uint64, []byte, bool) {
	handlerID, bytesRead := binary.Uvarint(msg)
	if bytesRead <= 0 {
		return 0, nil, false
	}
	return handlerID, msg[bytesRead:], true
}

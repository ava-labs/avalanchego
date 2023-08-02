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

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	ErrExistingAppProtocol = errors.New("existing app protocol")
	ErrUnrequestedResponse = errors.New("unrequested response")

	_ common.AppHandler    = (*Router)(nil)
	_ validators.Connector = (*Router)(nil)
)

// Router routes incoming application messages to the corresponding registered
// app handler. App messages must be made using the registered handler's
// corresponding Client.
type Router struct {
	log    logging.Logger
	sender common.AppSender

	lock                         sync.RWMutex
	handlers                     map[uint64]*responder
	pendingAppRequests           map[uint32]AppResponseCallback
	pendingCrossChainAppRequests map[uint32]CrossChainAppResponseCallback
	requestID                    uint32
	peers                        set.SampleableSet[ids.NodeID]
}

// NewRouter returns a new instance of Router
func NewRouter(log logging.Logger, sender common.AppSender) *Router {
	return &Router{
		log:                          log,
		sender:                       sender,
		handlers:                     make(map[uint64]*responder),
		pendingAppRequests:           make(map[uint32]AppResponseCallback),
		pendingCrossChainAppRequests: make(map[uint32]CrossChainAppResponseCallback),
	}
}

func (r *Router) Connected(_ context.Context, nodeID ids.NodeID, _ *version.Application) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.peers.Add(nodeID)
	return nil
}

func (r *Router) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.peers.Remove(nodeID)
	return nil
}

// RegisterAppProtocol reserves an identifier for an application protocol and
// returns a Client that can be used to send messages for the corresponding
// protocol.
func (r *Router) RegisterAppProtocol(handlerID uint64, handler Handler) (*Client, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	if _, ok := r.handlers[handlerID]; ok {
		return nil, fmt.Errorf("failed to register handler id %d: %w", handlerID, ErrExistingAppProtocol)
	}

	r.handlers[handlerID] = &responder{
		handlerID: handlerID,
		handler:   handler,
		log:       r.log,
		sender:    r.sender,
	}

	return &Client{
		handlerPrefix: binary.AppendUvarint(nil, handlerID),
		sender:        r.sender,
		router:        r,
	}, nil
}

func (r *Router) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
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

	return handler.AppRequest(ctx, nodeID, requestID, deadline, parsedMsg)
}

func (r *Router) AppRequestFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	callback, ok := r.clearAppRequest(requestID)
	if !ok {
		return ErrUnrequestedResponse
	}

	callback(nodeID, nil, ErrAppRequestFailed)
	return nil
}

func (r *Router) AppResponse(_ context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	callback, ok := r.clearAppRequest(requestID)
	if !ok {
		return ErrUnrequestedResponse
	}

	callback(nodeID, response, nil)
	return nil
}

func (r *Router) AppGossip(ctx context.Context, nodeID ids.NodeID, gossip []byte) error {
	parsedMsg, handler, ok := r.parse(gossip)
	if !ok {
		r.log.Debug("failed to process message",
			zap.Stringer("messageOp", message.AppGossipOp),
			zap.Stringer("nodeID", nodeID),
			zap.Binary("message", gossip),
		)
		return nil
	}

	return handler.AppGossip(ctx, nodeID, parsedMsg)
}

func (r *Router) CrossChainAppRequest(
	ctx context.Context,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	msg []byte,
) error {
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

	return handler.CrossChainAppRequest(ctx, chainID, requestID, deadline, parsedMsg)
}

func (r *Router) CrossChainAppRequestFailed(_ context.Context, chainID ids.ID, requestID uint32) error {
	callback, ok := r.clearCrossChainAppRequest(requestID)
	if !ok {
		return ErrUnrequestedResponse
	}

	callback(chainID, nil, ErrAppRequestFailed)
	return nil
}

func (r *Router) CrossChainAppResponse(_ context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	callback, ok := r.clearCrossChainAppRequest(requestID)
	if !ok {
		return ErrUnrequestedResponse
	}

	callback(chainID, response, nil)
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
func (r *Router) parse(msg []byte) ([]byte, *responder, bool) {
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
func (r *Router) clearAppRequest(requestID uint32) (AppResponseCallback, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	callback, ok := r.pendingAppRequests[requestID]
	delete(r.pendingAppRequests, requestID)
	return callback, ok
}

// Invariant: Assumes [r.lock] isn't held.
func (r *Router) clearCrossChainAppRequest(requestID uint32) (CrossChainAppResponseCallback, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	callback, ok := r.pendingCrossChainAppRequests[requestID]
	delete(r.pendingCrossChainAppRequests, requestID)
	return callback, ok
}

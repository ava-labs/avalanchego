// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	ErrUnregisteredHandler = errors.New("unregistered app protocol")
	ErrExistingAppProtocol = errors.New("existing app protocol")
	ErrUnrequestedResponse = errors.New("unrequested response")

	_ common.AppHandler    = (*Router)(nil)
	_ validators.Connector = (*Router)(nil)
)

// Router routes incoming application messages to the corresponding registered
// app handler. App messages must be made using the registered handler's
// corresponding Client.
type Router struct {
	nodeID ids.NodeID

	handlers                     map[uint8]responder
	pendingAppRequests           map[uint32]AppResponseCallback
	pendingCrossChainAppRequests map[uint32]CrossChainAppResponseCallback
	requestID                    uint32
	peers                        set.Set[ids.NodeID]
	lock                         sync.RWMutex
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

// NewRouter returns a new instance of Router
func NewRouter() *Router {
	return &Router{
		handlers:                     make(map[uint8]responder),
		pendingAppRequests:           make(map[uint32]AppResponseCallback),
		pendingCrossChainAppRequests: make(map[uint32]CrossChainAppResponseCallback),
	}
}

// RegisterAppProtocol reserves an identifier for an application protocol and
// returns a Client that can be used to send messages for the corresponding
// protocol.
func (r *Router) RegisterAppProtocol(handlerID uint8, handler Handler, sender common.AppSender) (*Client, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	if _, ok := r.handlers[handlerID]; ok {
		return nil, fmt.Errorf("failed to register handler id %d: %w", handlerID, ErrExistingAppProtocol)
	}

	client := &Client{
		handlerID: handlerID,
		sender:    sender,
		router:    r,
	}

	responder := responder{
		handler: handler,
		client:  client,
	}

	r.handlers[handlerID] = responder

	return client, nil
}

func (r *Router) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	r.lock.RLock()
	app, parsedMsg, handler, ok := r.parse(request)
	r.lock.RUnlock()
	if !ok {
		return fmt.Errorf("failed to process app request message for app protocol %d: %w", app, ErrUnregisteredHandler)
	}

	return handler.AppRequest(ctx, nodeID, requestID, deadline, parsedMsg)
}

func (r *Router) AppRequestFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	r.lock.RLock()
	callback, ok := r.clearAppRequest(requestID)
	r.lock.RUnlock()
	if !ok {
		return ErrUnrequestedResponse
	}

	callback(nodeID, nil, ErrAppRequestFailed)
	return nil
}

func (r *Router) AppResponse(_ context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	r.lock.RLock()
	callback, ok := r.clearAppRequest(requestID)
	r.lock.RUnlock()
	if !ok {
		return ErrUnrequestedResponse
	}

	callback(nodeID, response, nil)
	return nil
}

func (r *Router) AppGossip(ctx context.Context, nodeID ids.NodeID, gossip []byte) error {
	r.lock.RLock()
	app, parsedMsg, handler, ok := r.parse(gossip)
	r.lock.RUnlock()
	if !ok {
		return fmt.Errorf("failed to process gossip message for app protocol %d: %w", app, ErrUnregisteredHandler)
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
	r.lock.RLock()
	app, parsedMsg, handler, ok := r.parse(msg)
	r.lock.RUnlock()

	if !ok {
		return fmt.Errorf("failed to process cross chain app request message for app protocol %d: %w", app, ErrUnregisteredHandler)
	}

	return handler.CrossChainAppRequest(ctx, chainID, requestID, deadline, parsedMsg)
}

func (r *Router) CrossChainAppRequestFailed(_ context.Context, chainID ids.ID, requestID uint32) error {
	r.lock.RLock()
	callback, ok := r.clearCrossChainAppRequest(requestID)
	r.lock.RUnlock()
	if !ok {
		return ErrUnrequestedResponse
	}

	callback(chainID, nil, ErrAppRequestFailed)
	return nil
}

func (r *Router) CrossChainAppResponse(_ context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	r.lock.RLock()
	callback, ok := r.clearCrossChainAppRequest(requestID)
	r.lock.RUnlock()
	if !ok {
		return ErrUnrequestedResponse
	}

	callback(chainID, response, nil)
	return nil
}

// Parse parses a gossip or request message and maps it to a corresponding
// handler if present.
func (r *Router) parse(msg []byte) (byte, []byte, responder, bool) {
	if len(msg) == 0 {
		return 0, nil, responder{}, false
	}

	handlerID := msg[0]
	handler, ok := r.handlers[handlerID]
	return handlerID, msg[1:], handler, ok
}

func (r *Router) clearAppRequest(requestID uint32) (AppResponseCallback, bool) {
	callback, ok := r.pendingAppRequests[requestID]
	if !ok {
		return nil, false
	}

	delete(r.pendingAppRequests, requestID)
	return callback, true
}

func (r *Router) clearCrossChainAppRequest(requestID uint32) (CrossChainAppResponseCallback, bool) {
	callback, ok := r.pendingCrossChainAppRequests[requestID]
	if !ok {
		return nil, false
	}

	delete(r.pendingCrossChainAppRequests, requestID)
	return callback, true
}

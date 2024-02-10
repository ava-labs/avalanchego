// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

// Minimum amount of time to handle a request
const minRequestHandlingDuration = 100 * time.Millisecond

var (
	_ NetworkClient = (*networkClient)(nil)

	errAcquiringSemaphore = errors.New("error acquiring semaphore")
	errRequestFailed      = errors.New("request failed")
	errAppSendFailed      = errors.New("failed to send app message")
)

// NetworkClient defines ability to send request / response through the Network
type NetworkClient interface {
	// RequestAny synchronously sends request to an arbitrary peer with a
	// node version greater than or equal to minVersion.
	// Returns response bytes, the ID of the chosen peer, and ErrRequestFailed if
	// the request should be retried.
	RequestAny(
		ctx context.Context,
		request []byte,
	) (ids.NodeID, []byte, error)

	// Sends [request] to [nodeID] and returns the response.
	// Blocks until the number of outstanding requests is
	// below the limit before sending the request.
	Request(
		ctx context.Context,
		nodeID ids.NodeID,
		request []byte,
	) ([]byte, error)

	// The following declarations allow this interface to be embedded in the VM
	// to handle incoming responses from peers.

	// Always returns nil because the engine considers errors
	// returned from this function as fatal.
	AppResponse(context.Context, ids.NodeID, uint32, []byte) error

	// Always returns nil because the engine considers errors
	// returned from this function as fatal.
	AppRequestFailed(context.Context, ids.NodeID, uint32) error

	// Adds the given [nodeID] to the peer
	// list so that it can receive messages.
	// If [nodeID] is this node's ID, this is a no-op.
	Connected(context.Context, ids.NodeID, *version.Application) error

	// Removes given [nodeID] from the peer list.
	Disconnected(context.Context, ids.NodeID) error
}

type networkClient struct {
	lock sync.Mutex
	log  logging.Logger
	// requestID counter used to track outbound requests
	requestID uint32
	// requestID => handler for the response/failure
	outstandingRequestHandlers map[uint32]ResponseHandler
	// controls maximum number of active outbound requests
	activeRequests *semaphore.Weighted
	// tracking of peers & bandwidth usage
	peers *p2p.PeerTracker
	// For sending messages to peers
	appSender common.AppSender
}

func NewNetworkClient(
	appSender common.AppSender,
	myNodeID ids.NodeID,
	maxActiveRequests int64,
	log logging.Logger,
	metricsNamespace string,
	registerer prometheus.Registerer,
	minVersion *version.Application,
) (NetworkClient, error) {
	peerTracker, err := p2p.NewPeerTracker(
		log,
		metricsNamespace,
		registerer,
		set.Of(myNodeID),
		minVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer tracker: %w", err)
	}

	return &networkClient{
		appSender:                  appSender,
		outstandingRequestHandlers: make(map[uint32]ResponseHandler),
		activeRequests:             semaphore.NewWeighted(maxActiveRequests),
		peers:                      peerTracker,
		log:                        log,
	}, nil
}

func (c *networkClient) AppResponse(
	_ context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	response []byte,
) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.log.Info(
		"received AppResponse from peer",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Int("responseLen", len(response)),
	)

	handler, exists := c.getRequestHandler(requestID)
	if !exists {
		// Should never happen since the engine
		// should be managing outstanding requests
		c.log.Warn(
			"received response to unknown request",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Int("responseLen", len(response)),
		)
		return nil
	}
	handler.OnResponse(response)
	return nil
}

func (c *networkClient) AppRequestFailed(
	_ context.Context,
	nodeID ids.NodeID,
	requestID uint32,
) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.log.Info(
		"received AppRequestFailed from peer",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)

	handler, exists := c.getRequestHandler(requestID)
	if !exists {
		// Should never happen since the engine
		// should be managing outstanding requests
		c.log.Warn(
			"received request failed to unknown request",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}
	handler.OnFailure()
	return nil
}

// Returns the handler for [requestID] and marks the request as fulfilled.
// Returns false if there's no outstanding request with [requestID].
// Assumes [c.lock] is held.
func (c *networkClient) getRequestHandler(requestID uint32) (ResponseHandler, bool) {
	handler, exists := c.outstandingRequestHandlers[requestID]
	if !exists {
		return nil, false
	}
	// mark message as processed, release activeRequests slot
	delete(c.outstandingRequestHandlers, requestID)
	return handler, true
}

// If [errAppSendFailed] is returned this should be considered fatal.
func (c *networkClient) RequestAny(
	ctx context.Context,
	request []byte,
) (ids.NodeID, []byte, error) {
	// Take a slot from total [activeRequests] and block until a slot becomes available.
	if err := c.activeRequests.Acquire(ctx, 1); err != nil {
		return ids.EmptyNodeID, nil, errAcquiringSemaphore
	}
	defer c.activeRequests.Release(1)

	nodeID, responseChan, err := c.sendRequestAny(ctx, request)
	if err != nil {
		return ids.EmptyNodeID, nil, err
	}

	response, err := c.awaitResponse(ctx, nodeID, responseChan)
	return nodeID, response, err
}

func (c *networkClient) sendRequestAny(
	ctx context.Context,
	request []byte,
) (ids.NodeID, chan []byte, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	nodeID, ok := c.peers.SelectPeer()
	if !ok {
		numPeers := c.peers.Size()
		return ids.EmptyNodeID, nil, fmt.Errorf("no peers found from %d peers", numPeers)
	}

	responseChan, err := c.sendRequestLocked(ctx, nodeID, request)
	return nodeID, responseChan, err
}

// If [errAppSendFailed] is returned this should be considered fatal.
func (c *networkClient) Request(
	ctx context.Context,
	nodeID ids.NodeID,
	request []byte,
) ([]byte, error) {
	// Take a slot from total [activeRequests]
	// and block until a slot becomes available.
	if err := c.activeRequests.Acquire(ctx, 1); err != nil {
		return nil, errAcquiringSemaphore
	}
	defer c.activeRequests.Release(1)

	responseChan, err := c.sendRequest(ctx, nodeID, request)
	if err != nil {
		return nil, err
	}

	return c.awaitResponse(ctx, nodeID, responseChan)
}

func (c *networkClient) sendRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	request []byte,
) (chan []byte, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.sendRequestLocked(ctx, nodeID, request)
}

// Sends [request] to [nodeID] and returns a channel that will populate the
// response.
//
// If [errAppSendFailed] is returned this should be considered fatal.
//
// Assumes [nodeID] is never [c.myNodeID] since we guarantee [c.myNodeID] will
// not be added to [c.peers].
//
// Assumes [c.lock] is held.
func (c *networkClient) sendRequestLocked(
	ctx context.Context,
	nodeID ids.NodeID,
	request []byte,
) (chan []byte, error) {
	requestID := c.requestID
	c.requestID++

	c.log.Debug("sending request to peer",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Int("requestLen", len(request)),
	)
	c.peers.RegisterRequest(nodeID)

	// Send an app request to the peer.
	nodeIDs := set.Of(nodeID)
	if err := c.appSender.SendAppRequest(ctx, nodeIDs, requestID, request); err != nil {
		c.lock.Unlock()
		c.log.Fatal("failed to send app request",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Int("requestLen", len(request)),
			zap.Error(err),
		)
		return nil, fmt.Errorf("%w: %w", errAppSendFailed, err)
	}

	handler := newResponseHandler()
	c.outstandingRequestHandlers[requestID] = handler
	return handler.responseChan, nil
}

// awaitResponse from [nodeID] and returns the response.
//
// Returns an error if the request failed or [ctx] is canceled.
//
// Blocks until a response is received or the [ctx] is canceled fails.
//
// Assumes [nodeID] is never [c.myNodeID] since we guarantee [c.myNodeID] will
// not be added to [c.peers].
//
// Assumes [c.lock] is not held.
func (c *networkClient) awaitResponse(
	ctx context.Context,
	nodeID ids.NodeID,
	responseChan chan []byte,
) ([]byte, error) {
	var (
		response  []byte
		responded bool
		startTime = time.Now()
	)
	select {
	case <-ctx.Done():
		c.peers.RegisterFailure(nodeID)
		return nil, ctx.Err()
	case response, responded = <-responseChan:
	}
	if !responded {
		c.peers.RegisterFailure(nodeID)
		return nil, errRequestFailed
	}

	elapsedSeconds := time.Since(startTime).Seconds()
	bandwidth := float64(len(response)) / (elapsedSeconds + epsilon)
	c.peers.RegisterResponse(nodeID, bandwidth)

	c.log.Debug("received response from peer",
		zap.Stringer("nodeID", nodeID),
		zap.Int("responseLen", len(response)),
	)
	return response, nil
}

func (c *networkClient) Connected(
	_ context.Context,
	nodeID ids.NodeID,
	nodeVersion *version.Application,
) error {
	c.log.Debug("adding new peer", zap.Stringer("nodeID", nodeID))
	c.peers.Connected(nodeID, nodeVersion)
	return nil
}

func (c *networkClient) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	c.log.Debug("disconnecting peer", zap.Stringer("nodeID", nodeID))
	c.peers.Disconnected(nodeID)
	return nil
}

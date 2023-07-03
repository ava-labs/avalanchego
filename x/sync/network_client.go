// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"golang.org/x/sync/semaphore"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

// Minimum amount of time to handle a request
const minRequestHandlingDuration = 100 * time.Millisecond

var (
	_ networkClient = (*networkClientImpl)(nil)

	errAcquiringSemaphore = errors.New("error acquiring semaphore")
	errRequestFailed      = errors.New("request failed")
)

// networkClient defines ability to send request / response through the Network
type networkClient interface {
	// requestAny synchronously sends request to an arbitrary peer with a
	// node version greater than or equal to minVersion.
	// Returns response bytes, the ID of the chosen peer, and errRequestFailed if
	// the request should be retried.
	requestAny(ctx context.Context, minVersion *version.Application, request []byte) ([]byte, ids.NodeID, error)

	// request synchronously sends request to the selected nodeID.
	// Returns response bytes, and errRequestFailed if the request should be retried.
	request(ctx context.Context, nodeID ids.NodeID, request []byte) ([]byte, error)

	// trackBandwidth should be called for each valid response with the bandwidth
	// (length of response divided by request time), and with 0 if the response is invalid.
	trackBandwidth(nodeID ids.NodeID, bandwidth float64)

	// The following declarations allow this interface to be embedded in the VM
	// to handle incoming responses from peers.
	appResponse(context.Context, ids.NodeID, uint32, []byte) error
	appRequestFailed(context.Context, ids.NodeID, uint32) error
	connected(context.Context, ids.NodeID, *version.Application) error
	disconnected(context.Context, ids.NodeID) error
}

type networkClientImpl struct {
	lock sync.Mutex
	log  logging.Logger
	// This node's ID
	myNodeID ids.NodeID
	// requestID counter used to track outbound requests
	requestID uint32
	// requestID => handler for the response/failure
	outstandingRequestHandlers map[uint32]ResponseHandler
	// controls maximum number of active outbound requests
	activeRequests *semaphore.Weighted
	// tracking of peers & bandwidth usage
	peers *peerTracker
	// For sending messages to peers
	appSender common.AppSender
}

func NewNetworkClient(
	appSender common.AppSender,
	myNodeID ids.NodeID,
	maxActiveRequests int64,
	log logging.Logger,
) networkClient {
	return &networkClientImpl{
		appSender:                  appSender,
		myNodeID:                   myNodeID,
		outstandingRequestHandlers: make(map[uint32]ResponseHandler),
		activeRequests:             semaphore.NewWeighted(maxActiveRequests),
		peers:                      newPeerTracker(log),
		log:                        log,
	}
}

// Always returns nil because the engine considers errors
// returned from this function as fatal.
func (c *networkClientImpl) appResponse(
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

// Always returns nil because the engine considers errors
// returned from this function as fatal.
func (c *networkClientImpl) appRequestFailed(
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
func (c *networkClientImpl) getRequestHandler(requestID uint32) (ResponseHandler, bool) {
	handler, exists := c.outstandingRequestHandlers[requestID]
	if !exists {
		return nil, false
	}
	// mark message as processed, release activeRequests slot
	delete(c.outstandingRequestHandlers, requestID)
	return handler, true
}

// requestAny synchronously sends [request] to a randomly chosen peer with a
// version greater than or equal to [minVersion]. If [minVersion] is nil,
// the request is sent to any peer regardless of their version.
// May block until the number of outstanding requests decreases.
// Returns the node's response and the ID of the node.
func (c *networkClientImpl) requestAny(
	ctx context.Context,
	minVersion *version.Application,
	request []byte,
) ([]byte, ids.NodeID, error) {
	// Take a slot from total [activeRequests] and block until a slot becomes available.
	if err := c.activeRequests.Acquire(ctx, 1); err != nil {
		return nil, ids.EmptyNodeID, errAcquiringSemaphore
	}
	defer c.activeRequests.Release(1)

	c.lock.Lock()
	if nodeID, ok := c.peers.GetAnyPeer(minVersion); ok {
		// Note [c.request] releases [c.lock].
		response, err := c.get(ctx, nodeID, request)
		return response, nodeID, err
	}

	c.lock.Unlock()
	return nil, ids.EmptyNodeID, fmt.Errorf(
		"no peers found matching version %s out of %d peers",
		minVersion, c.peers.Size(),
	)
}

// Sends [request] to [nodeID] and returns the response.
// Blocks until the number of outstanding requests is
// below the limit before sending the request.
func (c *networkClientImpl) request(
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

	c.lock.Lock()
	// Note [c.request] releases [c.lock].
	return c.get(ctx, nodeID, request)
}

// Sends [get] to [nodeID] and returns the response.
// Returns an error if the get failed or [ctx] is canceled.
// Blocks until a response is received or the [ctx] is canceled fails.
// Releases active requests semaphore if there was an error in sending the get.
// Assumes [nodeID] is never [c.myNodeID] since we guarantee
// [c.myNodeID] will not be added to [c.peers].
// Assumes [c.lock] is held and unlocks [c.lock] before returning.
func (c *networkClientImpl) get(
	ctx context.Context,
	nodeID ids.NodeID,
	request []byte,
) ([]byte, error) {
	c.log.Debug("sending request to peer",
		zap.Stringer("nodeID", nodeID),
		zap.Int("requestLen", len(request)),
	)
	c.peers.TrackPeer(nodeID)

	requestID := c.requestID
	c.requestID++

	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)

	// Send an app request to the peer.
	if err := c.appSender.SendAppRequest(ctx, nodeIDs, requestID, request); err != nil {
		c.lock.Unlock()
		return nil, err
	}

	handler := newResponseHandler()
	c.outstandingRequestHandlers[requestID] = handler

	c.lock.Unlock() // unlock so response can be received

	var response []byte
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case response = <-handler.responseChan:
	}
	if handler.failed {
		return nil, errRequestFailed
	}

	c.log.Debug("received response from peer",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Int("responseLen", len(response)),
	)
	return response, nil
}

// connected adds the given [nodeID] to the peer
// list so that it can receive messages.
// If [nodeID] is [c.myNodeID], this is a no-op.
func (c *networkClientImpl) connected(
	_ context.Context,
	nodeID ids.NodeID,
	nodeVersion *version.Application,
) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	if nodeID == c.myNodeID {
		c.log.Debug("skipping registering self as peer")
		return nil
	}

	c.log.Debug("adding new peer", zap.Stringer("nodeID", nodeID))
	c.peers.Connected(nodeID, nodeVersion)
	return nil
}

// disconnected removes given [nodeID] from the peer list.
func (c *networkClientImpl) disconnected(_ context.Context, nodeID ids.NodeID) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	if nodeID == c.myNodeID {
		c.log.Debug("skipping deregistering self as peer")
		return nil
	}

	c.log.Debug("disconnecting peer", zap.Stringer("nodeID", nodeID))
	c.peers.Disconnected(nodeID)
	return nil
}

// Shutdown disconnects all peers
func (c *networkClientImpl) Shutdown() {
	c.lock.Lock()
	defer c.lock.Unlock()

	// reset peers
	// TODO danlaine: should we call [Disconnected] on each peer?
	c.peers = newPeerTracker(c.log)
}

func (c *networkClientImpl) trackBandwidth(nodeID ids.NodeID, bandwidth float64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.peers.TrackBandwidth(nodeID, bandwidth)
}

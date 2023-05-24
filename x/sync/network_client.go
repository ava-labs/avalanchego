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
	_ NetworkClient = &networkClient{}

	ErrAcquiringSemaphore = errors.New("error acquiring semaphore")
	ErrRequestFailed      = errors.New("request failed")
)

// NetworkClient defines ability to send request / response through the Network
type NetworkClient interface {
	// RequestAny synchronously sends request to an arbitrary peer with a
	// node version greater than or equal to minVersion.
	// Returns response bytes, the ID of the chosen peer, and ErrRequestFailed if
	// the request should be retried.
	RequestAny(ctx context.Context, minVersion *version.Application, request []byte) ([]byte, ids.NodeID, error)

	// Request synchronously sends request to the selected nodeID.
	// Returns response bytes, and ErrRequestFailed if the request should be retried.
	Request(ctx context.Context, nodeID ids.NodeID, request []byte) ([]byte, error)

	// TrackBandwidth should be called for each valid response with the bandwidth
	// (length of response divided by request time), and with 0 if the response is invalid.
	TrackBandwidth(nodeID ids.NodeID, bandwidth float64)

	// The following declarations allow this interface to be embedded in the VM
	// to handle incoming responses from peers.
	AppResponse(context.Context, ids.NodeID, uint32, []byte) error
	AppRequestFailed(context.Context, ids.NodeID, uint32) error
	Connected(context.Context, ids.NodeID, *version.Application) error
	Disconnected(context.Context, ids.NodeID) error
}

type networkClient struct {
	lock                       sync.Mutex                 // lock for mutating state of this Network struct
	myNodeID                   ids.NodeID                 // NodeID of this node
	requestID                  uint32                     // requestID counter used to track outbound requests
	outstandingRequestHandlers map[uint32]ResponseHandler // requestID => handler for the response/failure
	activeRequests             *semaphore.Weighted        // controls maximum number of active outbound requests
	peers                      *peerTracker               // tracking of peers & bandwidth
	appSender                  common.AppSender           // AppSender for sending messages
	log                        logging.Logger
}

func NewNetworkClient(
	appSender common.AppSender,
	myNodeID ids.NodeID,
	maxActiveRequests int64,
	log logging.Logger,
) NetworkClient {
	return &networkClient{
		appSender:                  appSender,
		myNodeID:                   myNodeID,
		outstandingRequestHandlers: make(map[uint32]ResponseHandler),
		activeRequests:             semaphore.NewWeighted(maxActiveRequests),
		peers:                      newPeerTracker(log),
		log:                        log,
	}
}

// AppResponse is called when this node receives a response from a peer.
// As the engine considers errors returned from this function as fatal,
// this function always returns nil.
func (c *networkClient) AppResponse(_ context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
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
		// Should never happen since the engine should be managing outstanding requests
		c.log.Error(
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

// AppRequestFailed can be called by the avalanchego -> VM in following cases:
// - node is benched
// - failed to send message to [nodeID] due to a network issue
// - timeout
// As the engine considers errors returned from this function as fatal,
// this function always returns nil.
func (c *networkClient) AppRequestFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.log.Info(
		"received AppRequestFailed from peer",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)

	handler, exists := c.getRequestHandler(requestID)
	if !exists {
		// Should never happen since the engine should be managing outstanding requests
		c.log.Error(
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
// This is called by either [AppResponse] or [AppRequestFailed].
// Assumes [c.lock] is held.
func (c *networkClient) getRequestHandler(requestID uint32) (ResponseHandler, bool) {
	handler, exists := c.outstandingRequestHandlers[requestID]
	if !exists {
		return nil, false
	}
	// mark message as processed, release activeRequests slot
	delete(c.outstandingRequestHandlers, requestID)
	c.activeRequests.Release(1)
	return handler, true
}

// RequestAny synchronously sends [request] to a randomly chosen peer with a
// node version greater than or equal to [minVersion]. If [minVersion] is nil,
// the request is sent to any peer regardless of their version.
// If the limit on active requests is reached, this function blocks until
// a slot becomes available.
// Returns the node's response and the ID of the node.
func (c *networkClient) RequestAny(
	ctx context.Context,
	minVersion *version.Application,
	request []byte,
) ([]byte, ids.NodeID, error) {
	// Take a slot from total [activeRequests] and block until a slot becomes available.
	if err := c.activeRequests.Acquire(ctx, 1); err != nil {
		return nil, ids.EmptyNodeID, ErrAcquiringSemaphore
	}

	c.lock.Lock()
	if nodeID, ok := c.peers.GetAnyPeer(minVersion); ok {
		response, err := c.request(ctx, nodeID, request)
		return response, nodeID, err
	}

	c.lock.Unlock()
	c.activeRequests.Release(1)
	return nil, ids.EmptyNodeID, fmt.Errorf("no peers found matching version %s out of %d peers", minVersion, c.peers.Size())
}

// Sends [request] to [nodeID] and registers a handler for the response/failure.
// If the limit on active requests is reached, this function blocks until
// a slot becomes available.
func (c *networkClient) Request(ctx context.Context, nodeID ids.NodeID, request []byte) ([]byte, error) {
	// TODO danlaine: is it possible for this condition to occur?
	if nodeID == ids.EmptyNodeID {
		return nil, fmt.Errorf("cannot send request to empty nodeID, nodeID=%s, requestLen=%d", nodeID, len(request))
	}

	// Take a slot from total [activeRequests] and block until a slot becomes available.
	if err := c.activeRequests.Acquire(ctx, 1); err != nil {
		return nil, ErrAcquiringSemaphore
	}

	c.lock.Lock()
	return c.request(ctx, nodeID, request)
}

// Sends [request] to [nodeID] and adds the response handler to [c.outstandingRequestHandlers]
// so that it can be invoked upon response/failure.
// Blocks until a response is received or the request fails.
// Assumes [nodeID] is never [c.myNodeID] since we guarantee [c.myNodeID] will not be added to [c.peers].
// Releases active requests semaphore if there was an error in sending the request.
// Returns an error if [appSender] is unable to make the request.
// Assumes [c.lock] is held and unlocks [c.lock] before returning.
func (c *networkClient) request(ctx context.Context, nodeID ids.NodeID, request []byte) ([]byte, error) {
	c.log.Debug("sending request to peer", zap.Stringer("nodeID", nodeID), zap.Int("requestLen", len(request)))
	c.peers.TrackPeer(nodeID)

	// generate requestID
	requestID := c.requestID
	c.requestID++

	handler := newResponseHandler()
	c.outstandingRequestHandlers[requestID] = handler

	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)

	// Send an app request to the peer.
	if err := c.appSender.SendAppRequest(ctx, nodeIDs, requestID, request); err != nil {
		// On failure, release the activeRequests slot and mark the message as processed.
		c.activeRequests.Release(1)
		delete(c.outstandingRequestHandlers, requestID)
		c.lock.Unlock()
		return nil, err
	}

	c.lock.Unlock() // unlock so response can be received

	var response []byte
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case response = <-handler.responseChan:
	}
	if handler.failed {
		return nil, ErrRequestFailed
	}

	c.log.Debug("received response from peer",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Int("responseLen", len(response)))
	return response, nil
}

// Connected adds the given nodeID to the peer list so that it can receive messages.
// If [nodeID] is [c.myNodeID], this is a no-op.
func (c *networkClient) Connected(_ context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
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

// Disconnected removes given [nodeID] from the peer list.
// TODO danlaine: should this be a no-op if [nodeID] is [c.myNodeID]?
func (c *networkClient) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.log.Debug("disconnecting peer", zap.Stringer("nodeID", nodeID))
	c.peers.Disconnected(nodeID)
	return nil
}

// Shutdown disconnects all peers
func (c *networkClient) Shutdown() {
	c.lock.Lock()
	defer c.lock.Unlock()

	// reset peers
	// TODO danlaine: should we call [Disconnected] on each peer?
	c.peers = newPeerTracker(c.log)
}

func (c *networkClient) TrackBandwidth(nodeID ids.NodeID, bandwidth float64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.peers.TrackBandwidth(nodeID, bandwidth)
}

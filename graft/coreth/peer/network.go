// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"

	"github.com/ava-labs/coreth/peer/stats"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

// Minimum amount of time to handle a request
const minRequestHandlingDuration = 100 * time.Millisecond

var (
	errAcquiringSemaphore                      = errors.New("error acquiring semaphore")
	_                     Network              = &network{}
	_                     validators.Connector = &network{}
	_                     common.AppHandler    = &network{}
)

type Network interface {
	validators.Connector
	common.AppHandler

	// RequestAny synchronously sends request to a randomly chosen peer with a
	// node version greater than or equal to minVersion.
	// Returns the ID of the chosen peer, and an error if the request could not
	// be sent to a peer with the desired [minVersion].
	RequestAny(minVersion *version.Application, message []byte, handler message.ResponseHandler) (ids.NodeID, error)

	// Request sends message to given nodeID, notifying handler when there's a response or timeout
	Request(nodeID ids.NodeID, message []byte, handler message.ResponseHandler) error

	// Gossip sends given gossip message to peers
	Gossip(gossip []byte) error

	// Shutdown stops all peer channel listeners and marks the node to have stopped
	// n.Start() can be called again but the peers will have to be reconnected
	// by calling OnPeerConnected for each peer
	Shutdown()

	// SetGossipHandler sets the provided gossip handler as the gossip handler
	SetGossipHandler(handler message.GossipHandler)

	// SetRequestHandler sets the provided request handler as the request handler
	SetRequestHandler(handler message.RequestHandler)

	// Size returns the size of the network in number of connected peers
	Size() uint32

	// TrackBandwidth should be called for each valid request with the bandwidth
	// (length of response divided by request time), and with 0 if the response is invalid.
	TrackBandwidth(nodeID ids.NodeID, bandwidth float64)
}

// network is an implementation of Network that processes message requests for
// each peer in linear fashion
type network struct {
	lock                       sync.RWMutex                       // lock for mutating state of this Network struct
	self                       ids.NodeID                         // NodeID of this node
	requestIDGen               uint32                             // requestID counter used to track outbound requests
	outstandingRequestHandlers map[uint32]message.ResponseHandler // maps avalanchego requestID => message.ResponseHandler
	activeRequests             *semaphore.Weighted                // controls maximum number of active outbound requests
	appSender                  common.AppSender                   // avalanchego AppSender for sending messages
	codec                      codec.Manager                      // Codec used for parsing messages
	requestHandler             message.RequestHandler             // maps request type => handler
	gossipHandler              message.GossipHandler              // maps gossip type => handler
	peers                      *peerTracker                       // tracking of peers & bandwidth
	stats                      stats.RequestHandlerStats          // Provide request handler metrics
}

func NewNetwork(appSender common.AppSender, codec codec.Manager, self ids.NodeID, maxActiveRequests int64) Network {
	return &network{
		appSender:                  appSender,
		codec:                      codec,
		self:                       self,
		outstandingRequestHandlers: make(map[uint32]message.ResponseHandler),
		activeRequests:             semaphore.NewWeighted(maxActiveRequests),
		gossipHandler:              message.NoopMempoolGossipHandler{},
		requestHandler:             message.NoopRequestHandler{},
		peers:                      NewPeerTracker(),
		stats:                      stats.NewRequestHandlerStats(),
	}
}

// RequestAny synchronously sends request to a randomly chosen peer with a
// node version greater than or equal to minVersion. If minVersion is nil,
// the request will be sent to any peer regardless of their version.
// Returns the ID of the chosen peer, and an error if the request could not
// be sent to a peer with the desired [minVersion].
func (n *network) RequestAny(minVersion *version.Application, request []byte, handler message.ResponseHandler) (ids.NodeID, error) {
	// Take a slot from total [activeRequests] and block until a slot becomes available.
	if err := n.activeRequests.Acquire(context.Background(), 1); err != nil {
		return ids.EmptyNodeID, errAcquiringSemaphore
	}

	n.lock.Lock()
	defer n.lock.Unlock()
	if nodeID, ok := n.peers.GetAnyPeer(minVersion); ok {
		return nodeID, n.request(nodeID, request, handler)
	}

	n.activeRequests.Release(1)
	return ids.EmptyNodeID, fmt.Errorf("no peers found matching version %s out of %d peers", minVersion, n.peers.Size())
}

// Request sends request message bytes to specified nodeID, notifying the responseHandler on response or failure
func (n *network) Request(nodeID ids.NodeID, request []byte, responseHandler message.ResponseHandler) error {
	if nodeID == ids.EmptyNodeID {
		return fmt.Errorf("cannot send request to empty nodeID, nodeID=%s, requestLen=%d", nodeID, len(request))
	}

	// Take a slot from total [activeRequests] and block until a slot becomes available.
	if err := n.activeRequests.Acquire(context.Background(), 1); err != nil {
		return errAcquiringSemaphore
	}

	n.lock.Lock()
	defer n.lock.Unlock()

	return n.request(nodeID, request, responseHandler)
}

// request sends request message bytes to specified nodeID and adds [responseHandler] to [outstandingRequestHandlers]
// so that it can be invoked when the network receives either a response or failure message.
// Assumes [nodeID] is never [self] since we guarantee [self] will not be added to the [peers] map.
// Releases active requests semaphore if there was an error in sending the request
// Returns an error if [appSender] is unable to make the request.
// Assumes write lock is held
func (n *network) request(nodeID ids.NodeID, request []byte, responseHandler message.ResponseHandler) error {
	log.Debug("sending request to peer", "nodeID", nodeID, "requestLen", len(request))
	n.peers.TrackPeer(nodeID)

	// generate requestID
	requestID := n.requestIDGen
	n.requestIDGen++

	n.outstandingRequestHandlers[requestID] = responseHandler

	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)

	// send app request to the peer
	// on failure: release the activeRequests slot, mark message as processed and return fatal error
	// Send app request to [nodeID].
	// On failure, release the slot from active requests and [outstandingRequestHandlers].
	if err := n.appSender.SendAppRequest(context.TODO(), nodeIDs, requestID, request); err != nil {
		n.activeRequests.Release(1)
		delete(n.outstandingRequestHandlers, requestID)
		return err
	}

	log.Debug("sent request message to peer", "nodeID", nodeID, "requestID", requestID)
	return nil
}

func (n *network) CrossChainAppRequest(_ context.Context, requestingChainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	return nil
}

func (n *network) CrossChainAppRequestFailed(_ context.Context, respondingChainID ids.ID, requestID uint32) error {
	return nil
}

func (n *network) CrossChainAppResponse(_ context.Context, respondingChainID ids.ID, requestID uint32, response []byte) error {
	return nil
}

// AppRequest is called by avalanchego -> VM when there is an incoming AppRequest from a peer
// error returned by this function is expected to be treated as fatal by the engine
// returns error if the requestHandler returns an error
// sends a response back to the sender if length of response returned by the handler is >0
// expects the deadline to not have been passed
func (n *network) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	n.lock.RLock()
	defer n.lock.RUnlock()

	log.Debug("received AppRequest from node", "nodeID", nodeID, "requestID", requestID, "requestLen", len(request))

	var req message.Request
	if _, err := n.codec.Unmarshal(request, &req); err != nil {
		log.Debug("failed to unmarshal app request", "nodeID", nodeID, "requestID", requestID, "requestLen", len(request), "err", err)
		return nil
	}

	// calculate how much time is left until the deadline
	timeTillDeadline := time.Until(deadline)
	n.stats.UpdateTimeUntilDeadline(timeTillDeadline)

	// bufferedDeadline is half the time till actual deadline so that the message has a reasonable chance
	// of completing its processing and sending the response to the peer.
	timeTillDeadline = time.Duration(timeTillDeadline.Nanoseconds() / 2)
	bufferedDeadline := time.Now().Add(timeTillDeadline)

	// check if we have enough time to handle this request
	if time.Until(bufferedDeadline) < minRequestHandlingDuration {
		// Drop the request if we already missed the deadline to respond.
		log.Debug("deadline to process AppRequest has expired, skipping", "nodeID", nodeID, "requestID", requestID, "req", req)
		n.stats.IncDeadlineDroppedRequest()
		return nil
	}

	log.Debug("processing incoming request", "nodeID", nodeID, "requestID", requestID, "req", req)
	// We make a new context here because we don't want to cancel the context
	// passed into n.AppSender.SendAppResponse below
	handleCtx, cancel := context.WithDeadline(context.Background(), bufferedDeadline)
	defer cancel()

	responseBytes, err := req.Handle(handleCtx, nodeID, requestID, n.requestHandler)
	switch {
	case err != nil && err != context.DeadlineExceeded:
		return err // Return a fatal error
	case responseBytes != nil:
		return n.appSender.SendAppResponse(ctx, nodeID, requestID, responseBytes) // Propagate fatal error
	default:
		return nil
	}
}

// AppResponse is invoked when there is a response received from a peer regarding a request
// Error returned by this function is expected to be treated as fatal by the engine
// If [requestID] is not known, this function will emit a log and return a nil error.
// If the response handler returns an error it is propagated as a fatal error.
func (n *network) AppResponse(_ context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	n.lock.Lock()
	defer n.lock.Unlock()

	log.Debug("received AppResponse from peer", "nodeID", nodeID, "requestID", requestID)

	handler, exists := n.getRequestHandler(requestID)
	if !exists {
		// Should never happen since the engine should be managing outstanding requests
		log.Error("received response to unknown request", "nodeID", nodeID, "requestID", requestID, "responseLen", len(response))
		return nil
	}

	return handler.OnResponse(nodeID, requestID, response)
}

// AppRequestFailed can be called by the avalanchego -> VM in following cases:
// - node is benched
// - failed to send message to [nodeID] due to a network issue
// - timeout
// error returned by this function is expected to be treated as fatal by the engine
// returns error only when the response handler returns an error
func (n *network) AppRequestFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	n.lock.Lock()
	defer n.lock.Unlock()
	log.Debug("received AppRequestFailed from peer", "nodeID", nodeID, "requestID", requestID)

	handler, exists := n.getRequestHandler(requestID)
	if !exists {
		// Should never happen since the engine should be managing outstanding requests
		log.Error("received request failed to unknown request", "nodeID", nodeID, "requestID", requestID)
		return nil
	}

	return handler.OnFailure(nodeID, requestID)
}

// getRequestHandler fetches the handler for [requestID] and marks the request with [requestID] as having been fulfilled.
// This is called by either [AppResponse] or [AppRequestFailed].
// assumes that the write lock is held.
func (n *network) getRequestHandler(requestID uint32) (message.ResponseHandler, bool) {
	handler, exists := n.outstandingRequestHandlers[requestID]
	if !exists {
		return nil, false
	}
	// mark message as processed, release activeRequests slot
	delete(n.outstandingRequestHandlers, requestID)
	n.activeRequests.Release(1)
	return handler, true
}

// Gossip sends given gossip message to peers
func (n *network) Gossip(gossip []byte) error {
	return n.appSender.SendAppGossip(context.TODO(), gossip)
}

// AppGossip is called by avalanchego -> VM when there is an incoming AppGossip from a peer
// error returned by this function is expected to be treated as fatal by the engine
// returns error if request could not be parsed as message.Request or when the requestHandler returns an error
func (n *network) AppGossip(_ context.Context, nodeID ids.NodeID, gossipBytes []byte) error {
	var gossipMsg message.GossipMessage
	if _, err := n.codec.Unmarshal(gossipBytes, &gossipMsg); err != nil {
		log.Debug("could not parse app gossip", "nodeID", nodeID, "gossipLen", len(gossipBytes), "err", err)
		return nil
	}

	log.Debug("processing AppGossip from node", "nodeID", nodeID, "msg", gossipMsg)
	return gossipMsg.Handle(n.gossipHandler, nodeID)
}

// Connected adds the given nodeID to the peer list so that it can receive messages
func (n *network) Connected(_ context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	log.Debug("adding new peer", "nodeID", nodeID)

	n.lock.Lock()
	defer n.lock.Unlock()

	if nodeID == n.self {
		log.Debug("skipping registering self as peer")
		return nil
	}

	n.peers.Connected(nodeID, nodeVersion)
	return nil
}

// Disconnected removes given [nodeID] from the peer list
func (n *network) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	log.Debug("disconnecting peer", "nodeID", nodeID)
	n.lock.Lock()
	defer n.lock.Unlock()

	n.peers.Disconnected(nodeID)
	return nil
}

// Shutdown disconnects all peers
func (n *network) Shutdown() {
	n.lock.Lock()
	defer n.lock.Unlock()

	// reset peers
	n.peers = NewPeerTracker()
}

func (n *network) SetGossipHandler(handler message.GossipHandler) {
	n.lock.Lock()
	defer n.lock.Unlock()

	n.gossipHandler = handler
}

func (n *network) SetRequestHandler(handler message.RequestHandler) {
	n.lock.Lock()
	defer n.lock.Unlock()

	n.requestHandler = handler
}

func (n *network) Size() uint32 {
	n.lock.RLock()
	defer n.lock.RUnlock()

	return uint32(n.peers.Size())
}

func (n *network) TrackBandwidth(nodeID ids.NodeID, bandwidth float64) {
	n.lock.Lock()
	defer n.lock.Unlock()

	n.peers.TrackBandwidth(nodeID, bandwidth)
}

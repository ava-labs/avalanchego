// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/coreth/plugin/evm/message"

	"github.com/ava-labs/avalanchego/snow/validators"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/sync/semaphore"
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

	// RequestAny synchronously sends request to the first connected peer that matches the specified minVersion in
	// random order.
	// A peer is considered a match if its version is greater than or equal to the specified minVersion
	// Returns errNoPeersMatchingVersion if no peer could be found matching specified version
	RequestAny(minVersion version.Application, message []byte, handler message.ResponseHandler) error

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
}

// network is an implementation of Network that processes message requests for
// each peer in linear fashion
type network struct {
	lock                          sync.RWMutex                        // lock for mutating state of this Network struct
	self                          ids.ShortID                         // NodeID of this node
	requestIDGen                  uint32                              // requestID counter used to track outbound requests
	outstandingResponseHandlerMap map[uint32]message.ResponseHandler  // maps avalanchego requestID => response handler
	activeRequests                *semaphore.Weighted                 // controls maximum number of active outbound requests
	appSender                     common.AppSender                    // avalanchego AppSender for sending messages
	codec                         codec.Manager                       // Codec used for parsing messages
	requestHandler                message.RequestHandler              // maps request type => handler
	gossipHandler                 message.GossipHandler               // maps gossip type => handler
	peers                         map[ids.ShortID]version.Application // maps nodeID => version.Version
}

func NewNetwork(appSender common.AppSender, codec codec.Manager, self ids.ShortID, maxActiveRequests int64) Network {
	return &network{
		appSender:                     appSender,
		codec:                         codec,
		self:                          self,
		outstandingResponseHandlerMap: make(map[uint32]message.ResponseHandler),
		peers:                         make(map[ids.ShortID]version.Application),
		activeRequests:                semaphore.NewWeighted(maxActiveRequests),
	}
}

// RequestAny sends given request to the first connected peer that matches the specified minVersion
// A peer is considered a match if its version is greater than or equal to the specified minVersion
// If minVersion is nil, then the request will be sent to any peer regardless of their version
// Returns a non-nil error if we were not able to send a request to a peer with >= [minVersion]
// or we fail to send a request to the selected peer.
func (n *network) RequestAny(minVersion version.Application, request []byte, handler message.ResponseHandler) error {
	// Take a slot from total [activeRequests] and block until a slot becomes available.
	if err := n.activeRequests.Acquire(context.Background(), 1); err != nil {
		return errAcquiringSemaphore
	}

	n.lock.Lock()
	defer n.lock.Unlock()

	for nodeID, nodeVersion := range n.peers {
		// map iteration is sufficiently random to avoid hitting same peer so here
		// we get a random peerID key that we compare minimum version that
		// we have
		if minVersion == nil || nodeVersion.Compare(minVersion) >= 0 {
			return n.request(nodeID, request, handler)
		}
	}

	n.activeRequests.Release(1)
	return fmt.Errorf("no peers found matching version %s out of %d peers", minVersion, len(n.peers))
}

// Request sends request message bytes to specified nodeID and adds [responseHandler] to [outstandingResponseHandlerMap]
// so that it can be invoked when the network receives either a response or failure message.
// Assumes [nodeID] is never [self] since we guarantee [self] will not be added to the [peers] map.
// Returns an error if [appSender] is unable to make the request.
// Assumes write lock is held
func (n *network) request(nodeID ids.ShortID, request []byte, responseHandler message.ResponseHandler) error {
	log.Debug("sending request to peer", "nodeID", nodeID, "requestLen", len(request))

	// generate requestID
	requestID := n.requestIDGen
	n.requestIDGen++

	n.outstandingResponseHandlerMap[requestID] = responseHandler

	nodeIDs := ids.NewShortSet(1)
	nodeIDs.Add(nodeID)

	// send app request to the peer
	// on failure: release the activeRequests slot, mark message as processed and return fatal error
	// Send app request to [nodeID].
	// On failure, release the slot from active requests and [outstandingResponseHandlerMap].
	if err := n.appSender.SendAppRequest(nodeIDs, requestID, request); err != nil {
		n.activeRequests.Release(1)
		delete(n.outstandingResponseHandlerMap, requestID)
		log.Error("could not send app message", "err", err, "nodeID", nodeID, "requestID", requestID)
		return err
	}

	log.Debug("sent request message to peer", "nodeID", nodeID, "requestID", requestID)
	return nil
}

// AppRequest is called by avalanchego -> VM when there is an incoming AppRequest from a peer
// error returned by this function is expected to be treated as fatal by the engine
// returns error if the requestHandler returns an error
// sends a response back to the sender if length of response returned by the handler is >0
// expects the deadline to not have been passed
func (n *network) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
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

	// bufferedDeadline is half the time till actual deadline so that the message has a reasonable chance
	// of completing its processing and sending the response to the peer.
	timeTillDeadline = time.Duration(timeTillDeadline.Nanoseconds() / 2)
	bufferedDeadline := time.Now().Add(timeTillDeadline)

	// check if we have enough time to handle this request
	if time.Until(bufferedDeadline) < minRequestHandlingDuration {
		// Drop the request if we already missed the deadline to respond.
		log.Debug("deadline to process AppRequest has expired, skipping", "nodeID", nodeID, "requestID", requestID, "type", req.Type())
		return nil
	}

	log.Debug("processing incoming request", "nodeID", nodeID, "requestID", requestID, "type", req.Type())
	ctx, cancel := context.WithDeadline(context.Background(), bufferedDeadline)
	defer cancel()

	responseBytes, err := req.Handle(ctx, nodeID, requestID, n.requestHandler)
	switch {
	case err != nil && err != context.DeadlineExceeded:
		return err // Return a fatal error
	case responseBytes != nil:
		return n.appSender.SendAppResponse(nodeID, requestID, responseBytes) // Propagate fatal error
	default:
		return nil
	}
}

// AppResponse is invoked when there is a response received from a peer regarding a request
// Error returned by this function is expected to be treated as fatal by the engine
// If [requestID] is not known, this function will emit a log and return a nil error.
// If the response handler returns an error it is propagated as a fatal error.
func (n *network) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
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
func (n *network) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
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
	handler, exists := n.outstandingResponseHandlerMap[requestID]
	if !exists {
		return nil, false
	}
	// mark message as processed, release activeRequests slot
	delete(n.outstandingResponseHandlerMap, requestID)
	n.activeRequests.Release(1)
	return handler, true
}

// Gossip sends given gossip message to peers
func (n *network) Gossip(gossip []byte) error {
	return n.appSender.SendAppGossip(gossip)
}

// AppGossip is called by avalanchego -> VM when there is an incoming AppGossip from a peer
// error returned by this function is expected to be treated as fatal by the engine
// returns error if request could not be parsed as message.Request or when the requestHandler returns an error
func (n *network) AppGossip(nodeID ids.ShortID, gossipBytes []byte) error {
	var gossipMsg message.Message
	if _, err := n.codec.Unmarshal(gossipBytes, &gossipMsg); err != nil {
		log.Debug("could not parse app gossip", "nodeID", nodeID, "gossipLen", len(gossipBytes), "err", err)
		return nil
	}

	log.Debug("processing AppGossip from node", "nodeID", nodeID, "type", gossipMsg.Type(), "gossipLen", len(gossipBytes))
	return gossipMsg.Handle(n.gossipHandler, nodeID)
}

// Connected adds the given nodeID to the peer list so that it can receive messages
func (n *network) Connected(nodeID ids.ShortID, nodeVersion version.Application) error {
	log.Debug("adding new peer", "nodeID", nodeID)

	n.lock.Lock()
	defer n.lock.Unlock()

	if nodeID == n.self {
		log.Debug("skipping registering self as peer")
		return nil
	}

	if storedVersion, exists := n.peers[nodeID]; exists {
		// Peer is already connected, update the version if it has changed.
		// Log a warning message since the consensus engine should never call Connected on a peer
		// that we have already marked as Connected.
		if nodeVersion.Compare(storedVersion) != 0 {
			n.peers[nodeID] = nodeVersion
			log.Warn("received Connected message for already connected peer, updating node version", "nodeID", nodeID, "storedVersion", storedVersion, "nodeVersion", nodeVersion)
		} else {
			log.Warn("ignoring peer connected event for already connected peer with identical version", "nodeID", nodeID)
		}
		return nil
	}

	n.peers[nodeID] = nodeVersion
	return nil
}

// Disconnected removes given [nodeID] from the peer list
func (n *network) Disconnected(nodeID ids.ShortID) error {
	log.Debug("disconnecting peer", "nodeID", nodeID)
	n.lock.Lock()
	defer n.lock.Unlock()

	// if this peer already exists, log a warning and ignore the request
	if _, exists := n.peers[nodeID]; !exists {
		// we're not connected to this peer, nothing to do here
		log.Warn("received peer disconnect request to unconnected peer", "nodeID", nodeID)
		return nil
	}

	delete(n.peers, nodeID)
	return nil
}

// Shutdown disconnects all peers
func (n *network) Shutdown() {
	n.lock.Lock()
	defer n.lock.Unlock()

	// reset peers map
	n.peers = make(map[ids.ShortID]version.Application)
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

	return uint32(len(n.peers))
}

// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	defaultCPUInterval = 15 * time.Second
)

var (
	errUnknownChain = errors.New("received message for unknown chain")

	_ Router = &ChainRouter{}
)

type requestEntry struct {
	// When this request was registered
	time time.Time
	// The type of request that was made
	msgType message.Op
}

// ChainRouter routes incoming messages from the validator network
// to the consensus engines that the messages are intended for.
// Note that consensus engines are uniquely identified by the ID of the chain
// that they are working on.
type ChainRouter struct {
	clock      mockable.Clock
	log        logging.Logger
	msgCreator message.Creator
	lock       sync.Mutex
	chains     map[ids.ID]*Handler

	// It is only safe to call [RegisterResponse] with the router lock held. Any
	// other calls to the timeout manager with the router lock held could cause
	// a deadlock because the timeout manager will call Benched and Unbenched.
	timeoutManager *timeout.Manager

	gossiper         *timer.Repeater
	intervalNotifier *timer.Repeater
	closeTimeout     time.Duration
	peers            ids.ShortSet
	// node ID --> chains that node is benched on
	// invariant: if a node is benched on any chain, it is treated as disconnected on all chains
	benched        map[ids.ShortID]ids.Set
	criticalChains ids.Set
	onFatal        func(exitCode int)
	metrics        *routerMetrics
	// Parameters for doing health checks
	healthConfig HealthConfig
	// aggregator of requests based on their time
	timedRequests linkedhashmap.LinkedHashmap
	// Last time at which there were no outstanding requests
	lastTimeNoOutstanding time.Time
	// Must only be accessed in method [createRequestID].
	// [lock] must be held when [requestIDBytes] is accessed.
	requestIDBytes []byte
}

// Initialize the router.
//
// When this router receives an incoming message, it cancels the timeout in
// [timeouts] associated with the request that caused the incoming message, if
// applicable.
//
// This router also fires a gossip event every [gossipFrequency] to the engine,
// notifying the engine it should gossip it's accepted set.
func (cr *ChainRouter) Initialize(
	nodeID ids.ShortID,
	log logging.Logger,
	msgCreator message.Creator,
	timeoutManager *timeout.Manager,
	gossipFrequency time.Duration,
	closeTimeout time.Duration,
	criticalChains ids.Set,
	onFatal func(exitCode int),
	healthConfig HealthConfig,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) error {
	cr.log = log
	cr.msgCreator = msgCreator
	cr.chains = make(map[ids.ID]*Handler)
	cr.timeoutManager = timeoutManager
	cr.gossiper = timer.NewRepeater(cr.Gossip, gossipFrequency)
	cr.intervalNotifier = timer.NewRepeater(cr.EndInterval, defaultCPUInterval)
	cr.closeTimeout = closeTimeout
	cr.benched = make(map[ids.ShortID]ids.Set)
	cr.criticalChains = criticalChains
	cr.onFatal = onFatal
	cr.timedRequests = linkedhashmap.New()
	cr.peers.Add(nodeID)
	cr.healthConfig = healthConfig
	cr.requestIDBytes = make([]byte, 2*hashing.HashLen+wrappers.IntLen+wrappers.ByteLen) // Validator ID, Chain ID, Request ID, Msg Type

	// Register metrics
	rMetrics, err := newRouterMetrics(metricsNamespace, metricsRegisterer)
	if err != nil {
		return err
	}
	cr.metrics = rMetrics

	go log.RecoverAndPanic(cr.gossiper.Dispatch)
	go log.RecoverAndPanic(cr.intervalNotifier.Dispatch)
	return nil
}

// Remove a request from [cr.requests]
// Assumes [cr.lock] is held
func (cr *ChainRouter) removeRequest(id ids.ID) {
	cr.timedRequests.Delete(id)
	cr.metrics.outstandingRequests.Set(float64(cr.timedRequests.Len()))
}

// RegisterRequest marks that we should expect to receive a reply from the given validator
// regarding the given chain and the reply should have the given requestID.
// The type of message we sent the validator was [msgType].
// Every registered request must be cleared either by receiving a valid reply
// and passing it to the appropriate chain or by a call to GetFailed, GetAncestorsFailed, etc.
// This method registers a timeout that calls such methods if we don't get a reply in time.
func (cr *ChainRouter) RegisterRequest(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	msgType message.Op,
) {
	cr.lock.Lock()
	// When we receive a response message type (Chits, Put, Accepted, etc.)
	// we validate that we actually sent the corresponding request.
	// Give this request a unique ID so we can do that validation.
	uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID, msgType)
	if cr.timedRequests.Len() == 0 {
		cr.lastTimeNoOutstanding = cr.clock.Time()
	}
	// Add to the set of unfulfilled requests
	cr.timedRequests.Put(uniqueRequestID, requestEntry{
		time:    cr.clock.Time(),
		msgType: msgType,
	})
	cr.metrics.outstandingRequests.Set(float64(cr.timedRequests.Len()))
	cr.lock.Unlock()

	// Register a timeout to fire if we don't get a reply in time.
	var inMsg message.InboundMessage
	var timeoutHandler func() // Called upon timeout
	switch msgType {
	case message.PullQuery, message.PushQuery:
		inMsg = cr.msgCreator.InternalQueryFailed(nodeID, chainID, requestID)
	case message.Get:
		inMsg = cr.msgCreator.InternalGetFailed(nodeID, chainID, requestID)
	case message.GetAncestors:
		inMsg = cr.msgCreator.InternalGetAncestorsFailed(nodeID, chainID, requestID)
	case message.GetAccepted:
		inMsg = cr.msgCreator.InternalGetAcceptedFailed(nodeID, chainID, requestID)
	case message.GetAcceptedFrontier:
		inMsg = cr.msgCreator.InternalGetAcceptedFrontierFailed(nodeID, chainID, requestID)
	case message.AppRequest:
		inMsg = cr.msgCreator.InternalAppRequestFailed(nodeID, chainID, requestID)
	default:
		// This should never happen
		cr.log.Error("expected message type to be one of GetMsg, PullQueryMsg, PushQueryMsg, GetAcceptedFrontierMsg, GetAcceptedMsg, AppRequestMsg, but got %s", msgType)
		return
	}

	timeoutHandler = func() { cr.HandleInbound(inMsg) }
	cr.timeoutManager.RegisterRequest(nodeID, chainID, msgType, uniqueRequestID, timeoutHandler)
}

// Shutdown shuts down this router
func (cr *ChainRouter) Shutdown() {
	cr.log.Info("shutting down chain router")
	cr.lock.Lock()
	prevChains := cr.chains
	cr.chains = map[ids.ID]*Handler{}
	cr.lock.Unlock()

	cr.gossiper.Stop()
	cr.intervalNotifier.Stop()

	for _, chain := range prevChains {
		chain.StartShutdown()
	}

	ticker := time.NewTicker(cr.closeTimeout)
	timedOut := false
	for _, chain := range prevChains {
		select {
		case <-chain.closed:
		case <-ticker.C:
			timedOut = true
		}
	}
	if timedOut {
		cr.log.Warn("timed out while shutting down the chains")
	}
	ticker.Stop()
}

// AddChain registers the specified chain so that incoming
// messages can be routed to it
func (cr *ChainRouter) AddChain(chain *Handler) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	chainID := chain.Context().ChainID
	cr.log.Debug("registering chain %s with chain router", chainID)
	chain.onCloseF = func() { cr.removeChain(chainID) }
	cr.chains[chainID] = chain

	for validatorID := range cr.peers {
		// If this validator is benched on any chain, treat them as disconnected on all chains
		if _, benched := cr.benched[validatorID]; !benched {
			chain.Connected(validatorID)
		}
	}
}

// RemoveChain removes the specified chain so that incoming
// messages can't be routed to it
func (cr *ChainRouter) removeChain(chainID ids.ID) {
	cr.lock.Lock()
	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("can't remove unknown chain %s", chainID)
		cr.lock.Unlock()
		return
	}
	delete(cr.chains, chainID)
	cr.lock.Unlock()

	chain.StartShutdown()

	ticker := time.NewTicker(cr.closeTimeout)
	select {
	case <-chain.closed:
	case <-ticker.C:
		chain.Context().Log.Warn("timed out while shutting down")
	}
	ticker.Stop()

	if cr.onFatal != nil && cr.criticalChains.Contains(chainID) {
		go cr.onFatal(1)
	}
}

func (cr *ChainRouter) markFullfilled(
	msgType message.Op,
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	matchReqType bool,
) (ids.ID, *requestEntry) {
	// Create the request ID of the request we sent that this message is (allegedly) in response to.
	uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID, msgType)

	// Mark that an outstanding request has been fulfilled
	requestIntf, exists := cr.timedRequests.Get(uniqueRequestID)
	if !exists {
		return uniqueRequestID, nil
	}
	request := requestIntf.(requestEntry)
	if matchReqType && request.msgType != msgType {
		return uniqueRequestID, nil
	}

	cr.removeRequest(uniqueRequestID)
	return uniqueRequestID, &request
}

func (cr *ChainRouter) HandleInbound(inMsg message.InboundMessage) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	nodeID := inMsg.NodeID()
	msgType := inMsg.Op()
	chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
	cr.log.AssertNoError(err)

	// AppGossip is the only message currently not containing a requestID
	// Here we assign the requestID already in use for gossiped containers
	// to allow a uniform handling of all messages
	var requestID uint32
	if msgType == message.AppGossip {
		requestID = constants.GossipMsgRequestID
	} else {
		requestID = inMsg.Get(message.RequestID).(uint32)
	}

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists || !chain.isValidator(nodeID) {
		inMsg.OnFinishedHandling()
		cr.log.Debug("Message %s from (%s. %s, %d) dropped. Error: %s",
			msgType.String(), nodeID, chainID, requestID, errUnknownChain)
		return
	}

	// Pass the message to the chain
	switch msgType {
	case // handle msgs with no deadlines
		message.GetAcceptedFrontier,
		message.GetAccepted,
		message.GetAncestors,
		message.Get,
		message.PullQuery,
		message.PushQuery,
		message.AppRequest:

		chain.push(inMsg)
		return

	case message.GetAcceptedFrontierFailed:
		// Create the request ID of the request we sent that this message is (allegedly) in response to.
		uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID, message.GetAcceptedFrontier)

		// Remove the outstanding request
		cr.removeRequest(uniqueRequestID)

		// Pass the response to the chain
		chain.push(inMsg)

	case message.AcceptedFrontier:
		uniqueRequestID, request := cr.markFullfilled(message.GetAcceptedFrontier, nodeID, chainID, requestID, false)
		if request == nil {
			// We didn't request this message. Ignore.
			inMsg.OnFinishedHandling()
			return
		}

		// Calculate how long it took [validatorID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, message.GetAcceptedFrontier, latency)

		// Pass the response to the chain
		chain.push(inMsg)
		return

	case message.GetAcceptedFailed:
		// Create the request ID of the request we sent that this message is (allegedly) in response to.
		uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID, message.GetAccepted)

		// Remove the outstanding request
		cr.removeRequest(uniqueRequestID)

		// Pass the response to the chain
		chain.push(inMsg)

	case message.Accepted:
		uniqueRequestID, request := cr.markFullfilled(message.GetAccepted, nodeID, chainID, requestID, false)
		if request == nil {
			// We didn't request this message. Ignore.
			inMsg.OnFinishedHandling()
			return
		}

		// Calculate how long it took [validatorID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, message.GetAccepted, latency)

		// Pass the response to the chain
		chain.push(inMsg)
		return

	case message.GetAncestorsFailed:
		// Create the request ID of the request we sent that this message is (allegedly) in response to.
		uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID, message.GetAncestors)

		// Remove the outstanding request
		cr.removeRequest(uniqueRequestID)

		// Pass the response to the chain
		chain.push(inMsg)

	case message.MultiPut:
		uniqueRequestID, request := cr.markFullfilled(message.GetAncestors, nodeID, chainID, requestID, false)
		if request == nil {
			// We didn't request this message. Ignore.
			inMsg.OnFinishedHandling()
			return
		}

		// Calculate how long it took [validatorID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, message.GetAncestors, latency)

		// Pass the response to the chain
		chain.push(inMsg)
		return

	case message.QueryFailed:
		// Create the request ID of the request we sent that this message is (allegedly) in response to.
		// Note that we treat PullQueryMsg and PushQueryMsg the same for the sake of creating request IDs.
		// See [createRequestID].
		uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID, message.PullQuery)

		// Remove the outstanding request
		cr.removeRequest(uniqueRequestID)

		// Pass the response to the chain
		chain.push(inMsg)

	case message.Chits:
		// Note that we treat PullQueryMsg and PushQueryMsg the same for the sake of creating request IDs.
		// See [createRequestID].
		uniqueRequestID, request := cr.markFullfilled(message.PullQuery, nodeID, chainID, requestID, false)
		if request == nil {
			// We didn't request this message. Ignore.
			inMsg.OnFinishedHandling()
			return
		}

		// Calculate how long it took [validatorID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, request.msgType, latency)

		// Pass the response to the chain
		chain.push(inMsg)
		return

	case message.GetFailed:
		// Create the request ID of the request we sent that this message is (allegedly) in response to.
		uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID, message.Get)

		// Remove the outstanding request
		cr.removeRequest(uniqueRequestID)

		// Pass the response to the chain
		chain.push(inMsg)

	case message.Put:
		// If this is a gossip message, pass to the chain
		if requestID == constants.GossipMsgRequestID {
			chain.push(inMsg)
			return
		}

		uniqueRequestID, request := cr.markFullfilled(message.Get, nodeID, chainID, requestID, false)
		if request == nil {
			// We didn't request this message. Ignore.
			inMsg.OnFinishedHandling()
			return
		}

		// Calculate how long it took [nodeID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, message.Get, latency)

		// Pass the response to the chain
		chain.push(inMsg)
		return

	case message.AppResponse:
		uniqueRequestID, request := cr.markFullfilled(message.AppRequest, nodeID, chainID, requestID, true)
		if request == nil {
			// We didn't request this message, or it was the wrong type. Ignore.
			inMsg.OnFinishedHandling()
			return
		}

		// Calculate how long it took [nodeID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, request.msgType, latency)

		// Pass the response to the chain
		chain.push(inMsg)
		return

	case message.AppGossip:
		chain.push(inMsg)
		return

	default:
		cr.log.Error("Unhandled message type %v. Dropping it.", msgType)
		return
	}
}

// Connected routes an incoming notification that a validator was just connected
func (cr *ChainRouter) Connected(validatorID ids.ShortID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	cr.peers.Add(validatorID)
	// If this validator is benched on any chain, treat them as disconnected on all chains
	if _, benched := cr.benched[validatorID]; benched {
		return
	}

	// TODO: fire up an event when validator state changes i.e when they leave set, disconnect.
	// we cannot put a subnet-only validator check here since Disconnected would not be handled properly.
	for _, chain := range cr.chains {
		chain.Connected(validatorID)
	}
}

// Disconnected routes an incoming notification that a validator was connected
func (cr *ChainRouter) Disconnected(validatorID ids.ShortID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	cr.peers.Remove(validatorID)
	if _, benched := cr.benched[validatorID]; benched {
		return
	}

	// TODO: fire up an event when validator state changes i.e when they leave set, disconnect.
	// we cannot put a subnet-only validator check here since if a validator connects then it leaves validator-set, it would not be disconnected properly.
	for _, chain := range cr.chains {
		chain.Disconnected(validatorID)
	}
}

// Benched routes an incoming notification that a validator was benched
func (cr *ChainRouter) Benched(chainID ids.ID, validatorID ids.ShortID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	benchedChains, exists := cr.benched[validatorID]
	benchedChains.Add(chainID)
	cr.benched[validatorID] = benchedChains
	if exists || !cr.peers.Contains(validatorID) {
		// If the set already existed, then the node was previously benched.
		return
	}

	for _, chain := range cr.chains {
		chain.Disconnected(validatorID)
	}
}

// Unbenched routes an incoming notification that a validator was just unbenched
func (cr *ChainRouter) Unbenched(chainID ids.ID, validatorID ids.ShortID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	benchedChains := cr.benched[validatorID]
	benchedChains.Remove(chainID)
	if benchedChains.Len() == 0 {
		delete(cr.benched, validatorID)
	} else {
		cr.benched[validatorID] = benchedChains
		return // This node is still benched
	}

	if !cr.peers.Contains(validatorID) {
		return
	}

	for _, chain := range cr.chains {
		chain.Connected(validatorID)
	}
}

// Gossip accepted containers
func (cr *ChainRouter) Gossip() {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	for _, chain := range cr.chains {
		chain.Gossip()
	}
}

// EndInterval notifies the chains that the current CPU interval has ended
// TODO remove?
func (cr *ChainRouter) EndInterval() {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	for _, chain := range cr.chains {
		chain.endInterval()
	}
}

// HealthCheck returns results of router health checks. Returns:
// 1) Information about health check results
// 2) An error if the health check reports unhealthy
func (cr *ChainRouter) HealthCheck() (interface{}, error) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	numOutstandingReqs := cr.timedRequests.Len()
	isOutstandingReqs := numOutstandingReqs <= cr.healthConfig.MaxOutstandingRequests
	healthy := isOutstandingReqs
	details := map[string]interface{}{
		"outstandingRequests": numOutstandingReqs,
	}

	// check for long running requests
	now := cr.clock.Time()
	processingRequest := now
	if _, longestRunning, exists := cr.timedRequests.Oldest(); exists {
		processingRequest = longestRunning.(requestEntry).time
	}
	timeReqRunning := now.Sub(processingRequest)
	isOutstanding := timeReqRunning <= cr.healthConfig.MaxOutstandingDuration
	healthy = healthy && isOutstanding
	details["longestRunningRequest"] = timeReqRunning.String()
	cr.metrics.longestRunningRequest.Set(float64(timeReqRunning))

	if !healthy {
		var errorReasons []string
		if !isOutstandingReqs {
			errorReasons = append(errorReasons, fmt.Sprintf("number of outstanding requests %d > %d", numOutstandingReqs, cr.healthConfig.MaxOutstandingRequests))
		}
		if !isOutstanding {
			errorReasons = append(errorReasons, fmt.Sprintf("time for outstanding requests %s > %s", timeReqRunning, cr.healthConfig.MaxOutstandingDuration))
		}
		// The router is not healthy
		return details, fmt.Errorf("the router is not healthy reason: %s", strings.Join(errorReasons, ", "))
	}
	return details, nil
}

// Assumes [cr.lock] is held.
// Assumes [constants.msgType] is an alias of byte.
func (cr *ChainRouter) createRequestID(nodeID ids.ShortID, chainID ids.ID, requestID uint32, msgType message.Op) ids.ID {
	// If we receive Chits, they may be in response to either a PullQuery or a PushQuery.
	// Treat PullQuery and PushQuery messages as being the same for the sake of validating
	// that Chits we receive are in response to a query (either push or pull) that we sent.
	if msgType == message.PullQuery {
		msgType = message.PushQuery
	}
	copy(cr.requestIDBytes, nodeID[:])
	copy(cr.requestIDBytes[hashing.HashLen:], chainID[:])
	binary.BigEndian.PutUint32(cr.requestIDBytes[2*hashing.HashLen:], requestID)
	cr.requestIDBytes[2*hashing.HashLen+wrappers.IntLen] = byte(msgType)
	return hashing.ComputeHash256Array(cr.requestIDBytes)
}

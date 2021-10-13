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
	msgType constants.MsgType
}

// ChainRouter routes incoming messages from the validator network
// to the consensus engines that the messages are intended for.
// Note that consensus engines are uniquely identified by the ID of the chain
// that they are working on.
type ChainRouter struct {
	clock  timer.Clock
	log    logging.Logger
	lock   sync.Mutex
	chains map[ids.ID]*Handler

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
	msgType constants.MsgType,
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
	var timeoutHandler func() // Called upon timeout
	switch msgType {
	case constants.PullQueryMsg, constants.PushQueryMsg:
		timeoutHandler = func() { cr.QueryFailed(nodeID, chainID, requestID) }
	case constants.GetMsg:
		timeoutHandler = func() { cr.GetFailed(nodeID, chainID, requestID) }
	case constants.GetAncestorsMsg:
		timeoutHandler = func() { cr.GetAncestorsFailed(nodeID, chainID, requestID) }
	case constants.GetAcceptedMsg:
		timeoutHandler = func() { cr.GetAcceptedFailed(nodeID, chainID, requestID) }
	case constants.GetAcceptedFrontierMsg:
		timeoutHandler = func() { cr.GetAcceptedFrontierFailed(nodeID, chainID, requestID) }
	case constants.AppRequestMsg:
		timeoutHandler = func() { cr.AppRequestFailed(nodeID, chainID, requestID) }
	default:
		// This should never happen
		cr.log.Error("expected message type to be one of GetMsg, PullQueryMsg, PushQueryMsg, GetAcceptedFrontierMsg, GetAcceptedMsg, AppRequestMsg, but got %s", msgType)
		return
	}
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

func (cr *ChainRouter) checkMsg(inMsg message.InboundMessage) error {
	// check relevant messages fields are well formed, before submitting them to the engine

	switch inMsg.Op() {
	case message.AcceptedFrontier:
		_, err := message.DecodeContainerIDs(inMsg)
		return err
	case message.GetAccepted:
		_, err := message.DecodeContainerIDs(inMsg)
		return err
	case message.Accepted:
		_, err := message.DecodeContainerIDs(inMsg)
		return err
	case message.GetAncestors:
		_, err := ids.ToID(inMsg.Get(message.ContainerID).([]byte))
		cr.log.AssertNoError(err)
		return err
	case message.MultiPut:
		if _, ok := inMsg.Get(message.MultiContainerBytes).([][]byte); !ok {
			return fmt.Errorf("malformed Multiput message. Could not parse MultiContainerBytes")
		}
		return nil
	case message.Put:
		if _, err := ids.ToID(inMsg.Get(message.ContainerID).([]byte)); err != nil {
			cr.log.AssertNoError(err)
			return err
		}
		if _, ok := inMsg.Get(message.ContainerBytes).([]byte); !ok {
			return fmt.Errorf("malformed Put message. Could not parse ContainerBytes")
		}
		return nil
	case message.Get:
		_, err := ids.ToID(inMsg.Get(message.ContainerID).([]byte))
		cr.log.AssertNoError(err)
		return err
	case message.PullQuery:
		_, err := ids.ToID(inMsg.Get(message.ContainerID).([]byte))
		cr.log.AssertNoError(err)
		return err
	case message.PushQuery:
		if _, err := ids.ToID(inMsg.Get(message.ContainerID).([]byte)); err != nil {
			cr.log.AssertNoError(err)
			return err
		}
		if _, ok := inMsg.Get(message.ContainerBytes).([]byte); !ok {
			return fmt.Errorf("malformed PushQuery message. Could not parse ContainerBytes")
		}
		return nil
	case message.Chits:
		_, err := message.DecodeContainerIDs(inMsg)
		return err
	case message.AppRequest:
		if _, ok := inMsg.Get(message.AppRequestBytes).([]byte); !ok {
			return fmt.Errorf("malformed AppRequest message. Could not parse AppRequestBytes")
		}
		return nil
	case message.AppResponse:
		if _, ok := inMsg.Get(message.AppResponseBytes).([]byte); !ok {
			return fmt.Errorf("malformed AppResponse message. Could not parse AppResponseBytes")
		}
		return nil
	case message.AppGossip:
		if _, ok := inMsg.Get(message.AppGossipBytes).([]byte); !ok {
			return fmt.Errorf("malformed AppGossip message. Could not parse AppGossipBytes")
		}
		return nil
	}

	// messages lacking explicit checks are deemed good
	return nil
}

func (cr *ChainRouter) markFullfilled(
	msgType constants.MsgType,
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

	cr.timedRequests.Delete(uniqueRequestID)
	return uniqueRequestID, &request
}

// A map from Op to constants.MsgType
func opToConstant(op message.Op) constants.MsgType {
	switch op {
	case message.GetAcceptedFrontier:
		return constants.GetAcceptedFrontierMsg
	case message.AcceptedFrontier:
		return constants.AcceptedFrontierMsg
	case message.GetAccepted:
		return constants.GetAcceptedMsg
	case message.Accepted:
		return constants.AcceptedMsg
	case message.GetAncestors:
		return constants.GetAncestorsMsg
	case message.MultiPut:
		return constants.MultiPutMsg
	case message.Get:
		return constants.GetMsg
	case message.Put:
		return constants.PutMsg
	case message.PullQuery:
		return constants.PullQueryMsg
	case message.PushQuery:
		return constants.PushQueryMsg
	case message.Chits:
		return constants.ChitsMsg
	case message.AppRequest:
		return constants.AppRequestMsg
	case message.AppResponse:
		return constants.AppResponseMsg
	case message.AppGossip:
		return constants.AppGossipMsg
	default:
		return constants.NullMsg
	}
}

func (cr *ChainRouter) HandleInbound(
	inMsg message.InboundMessage,
	nodeID ids.ShortID,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

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
		onFinishedHandling()
		cr.log.Debug("Message %s from (%s. %s, %d) dropped. Error: %s",
			msgType.String(), nodeID, chainID, requestID, errUnknownChain)
		return
	}

	if err := cr.checkMsg(inMsg); err != nil {
		cr.log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: %s",
			msgType.String(), nodeID, chainID, requestID, err)
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

		chain.PushMsgWithDeadline(opToConstant(msgType), inMsg, nodeID, requestID, onFinishedHandling)
		return

	case message.AcceptedFrontier:
		uniqueRequestID, request := cr.markFullfilled(constants.GetAcceptedFrontierMsg, nodeID, chainID, requestID, false)
		if request == nil {
			// We didn't request this message. Ignore.
			onFinishedHandling()
			return
		}

		// Calculate how long it took [validatorID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, constants.GetAcceptedFrontierMsg, latency)

		// Pass the response to the chain
		chain.PushMsgWithoutDeadline(opToConstant(msgType), inMsg, nodeID, requestID, onFinishedHandling)
		return

	case message.Accepted:
		uniqueRequestID, request := cr.markFullfilled(constants.GetAcceptedMsg, nodeID, chainID, requestID, false)
		if request == nil {
			// We didn't request this message. Ignore.
			onFinishedHandling()
			return
		}

		// Calculate how long it took [validatorID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, constants.GetAcceptedMsg, latency)

		// Pass the response to the chain
		chain.PushMsgWithoutDeadline(opToConstant(msgType), inMsg, nodeID, requestID, onFinishedHandling)
		return

	case message.MultiPut:
		uniqueRequestID, request := cr.markFullfilled(constants.GetAncestorsMsg, nodeID, chainID, requestID, false)
		if request == nil {
			// We didn't request this message. Ignore.
			onFinishedHandling()
			return
		}

		// Calculate how long it took [validatorID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, constants.GetAncestorsMsg, latency)

		// Pass the response to the chain
		chain.PushMsgWithoutDeadline(opToConstant(msgType), inMsg, nodeID, requestID, onFinishedHandling)
		return

	case message.Chits:
		// Note that we treat PullQueryMsg and PushQueryMsg the same for the sake of creating request IDs.
		// See [createRequestID].
		uniqueRequestID, request := cr.markFullfilled(constants.PullQueryMsg, nodeID, chainID, requestID, false)
		if request == nil {
			// We didn't request this message. Ignore.
			onFinishedHandling()
			return
		}

		// Calculate how long it took [validatorID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, request.msgType, latency)

		// Pass the response to the chain
		chain.PushMsgWithoutDeadline(opToConstant(msgType), inMsg, nodeID, requestID, onFinishedHandling)
		return

	case message.Put:
		// If this is a gossip message, pass to the chain
		if requestID == constants.GossipMsgRequestID {
			chain.PushMsgWithoutDeadline(opToConstant(msgType), inMsg, nodeID, requestID, onFinishedHandling)
			return
		}

		uniqueRequestID, request := cr.markFullfilled(constants.GetMsg, nodeID, chainID, requestID, false)
		if request == nil {
			// We didn't request this message. Ignore.
			onFinishedHandling()
			return
		}

		// Calculate how long it took [nodeID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, constants.GetMsg, latency)

		// Pass the response to the chain
		chain.PushMsgWithoutDeadline(opToConstant(msgType), inMsg, nodeID, requestID, onFinishedHandling)
		return

	case message.AppResponse:
		uniqueRequestID, request := cr.markFullfilled(constants.AppRequestMsg, nodeID, chainID, requestID, true)
		if request == nil {
			// We didn't request this message, or it was the wrong type. Ignore.
			onFinishedHandling()
			return
		}

		// Calculate how long it took [nodeID] to reply
		latency := cr.clock.Time().Sub(request.time)

		// Tell the timeout manager we got a response
		cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, request.msgType, latency)

		// Pass the response to the chain
		chain.PushMsgWithoutDeadline(opToConstant(msgType), inMsg, nodeID, requestID, onFinishedHandling)
		return
	case message.AppGossip:
		chain.PushMsgWithoutDeadline(opToConstant(msgType), inMsg, nodeID, requestID, onFinishedHandling)
		return

	default:
		cr.log.Error("Unhandled message type %v. Dropping it.", msgType)
		return
	}
}

// GetAcceptedFrontierFailed routes an incoming GetAcceptedFrontierFailed
// request from the validator with ID [validatorID] to the consensus engine
// working on the chain with ID [chainID]
func (cr *ChainRouter) GetAcceptedFrontierFailed(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Create the request ID of the request we sent that this message is (allegedly) in response to.
	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID, constants.GetAcceptedFrontierMsg)

	// Remove the outstanding request
	cr.removeRequest(uniqueRequestID)

	chain, exists := cr.chains[chainID]
	if !exists {
		// Should only happen if node is shutting down
		cr.log.Debug("GetAcceptedFrontierFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
		return
	}
	// Pass the response to the chain
	chain.GetAcceptedFrontierFailed(validatorID, requestID)
}

// GetAcceptedFailed routes an incoming GetAcceptedFailed request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (cr *ChainRouter) GetAcceptedFailed(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Create the request ID of the request we sent that this message is (allegedly) in response to.
	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID, constants.GetAcceptedMsg)

	// Remove the outstanding request
	cr.removeRequest(uniqueRequestID)

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		// Should only happen when shutting down
		cr.log.Debug("GetAcceptedFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
		return
	}

	// Pass the response to the chain
	chain.GetAcceptedFailed(validatorID, requestID)
}

// GetAncestorsFailed routes an incoming GetAncestorsFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) GetAncestorsFailed(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Create the request ID of the request we sent that this message is (allegedly) in response to.
	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID, constants.GetAncestorsMsg)

	// Remove the outstanding request
	cr.removeRequest(uniqueRequestID)

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		// Should only happen if shutting down
		cr.log.Debug("GetAncestorsFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
		return
	}

	// Pass the response to the chain
	chain.GetAncestorsFailed(validatorID, requestID)
}

// GetFailed routes an incoming GetFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) GetFailed(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Create the request ID of the request we sent that this message is (allegedly) in response to.
	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID, constants.GetMsg)

	// Remove the outstanding request
	cr.removeRequest(uniqueRequestID)

	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("GetFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
		return
	}

	// Pass the response to the chain
	chain.GetFailed(validatorID, requestID)
}

// AppRequestFailed notifies the given chain that it will not receive a response to its request
// with the given ID to the given node.
func (cr *ChainRouter) AppRequestFailed(nodeID ids.ShortID, chainID ids.ID, requestID uint32) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID, constants.AppRequestMsg)

	// Remove the outstanding request
	cr.removeRequest(uniqueRequestID)

	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("AppRequestFailed(%s, %s, %d) dropped due to unknown chain", nodeID, chainID, requestID)
		return
	}
	// Pass the response to the chain
	chain.AppRequestFailed(nodeID, requestID)
}

// QueryFailed routes an incoming QueryFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) QueryFailed(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Create the request ID of the request we sent that this message is (allegedly) in response to.
	// Note that we treat PullQueryMsg and PushQueryMsg the same for the sake of creating request IDs.
	// See [createRequestID].
	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID, constants.PullQueryMsg)

	// Remove the outstanding request
	cr.removeRequest(uniqueRequestID)

	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("QueryFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
		return
	}
	// Pass the response to the chain
	chain.QueryFailed(validatorID, requestID)
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
func (cr *ChainRouter) createRequestID(nodeID ids.ShortID, chainID ids.ID, requestID uint32, msgType constants.MsgType) ids.ID {
	// If we receive Chits, they may be in response to either a PullQuery or a PushQuery.
	// Treat PullQuery and PushQuery messages as being the same for the sake of validating
	// that Chits we receive are in response to a query (either push or pull) that we sent.
	if msgType == constants.PullQueryMsg {
		msgType = constants.PushQueryMsg
	}
	copy(cr.requestIDBytes, nodeID[:])
	copy(cr.requestIDBytes[hashing.HashLen:], chainID[:])
	binary.BigEndian.PutUint32(cr.requestIDBytes[2*hashing.HashLen:], requestID)
	cr.requestIDBytes[2*hashing.HashLen+wrappers.IntLen] = byte(msgType)
	return hashing.ComputeHash256Array(cr.requestIDBytes)
}

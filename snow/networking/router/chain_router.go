// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errUnknownChain = errors.New("received message for unknown chain")

	_ Router = &ChainRouter{}
)

type requestEntry struct {
	// When this request was registered
	time time.Time
	// The type of request that was made
	op message.Op
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
	chains     map[ids.ID]handler.Handler

	// It is only safe to call [RegisterResponse] with the router lock held. Any
	// other calls to the timeout manager with the router lock held could cause
	// a deadlock because the timeout manager will call Benched and Unbenched.
	timeoutManager *timeout.Manager

	closeTimeout time.Duration
	peers        map[ids.ShortID]version.Application
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
	// Must only be accessed in method [createRequestID].
	// [lock] must be held when [requestIDBytes] is accessed.
	requestIDBytes []byte
}

// Initialize the router.
//
// When this router receives an incoming message, it cancels the timeout in
// [timeouts] associated with the request that caused the incoming message, if
// applicable.
func (cr *ChainRouter) Initialize(
	nodeID ids.ShortID,
	log logging.Logger,
	msgCreator message.Creator,
	timeoutManager *timeout.Manager,
	closeTimeout time.Duration,
	criticalChains ids.Set,
	onFatal func(exitCode int),
	healthConfig HealthConfig,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) error {
	cr.log = log
	cr.msgCreator = msgCreator
	cr.chains = make(map[ids.ID]handler.Handler)
	cr.timeoutManager = timeoutManager
	cr.closeTimeout = closeTimeout
	cr.benched = make(map[ids.ShortID]ids.Set)
	cr.criticalChains = criticalChains
	cr.onFatal = onFatal
	cr.timedRequests = linkedhashmap.New()
	cr.peers = make(map[ids.ShortID]version.Application)
	cr.peers[nodeID] = version.CurrentApp
	cr.healthConfig = healthConfig
	cr.requestIDBytes = make([]byte, hashing.AddrLen+hashing.HashLen+wrappers.IntLen+wrappers.ByteLen) // Validator ID, Chain ID, Request ID, Msg Type

	// Register metrics
	rMetrics, err := newRouterMetrics(metricsNamespace, metricsRegisterer)
	if err != nil {
		return err
	}
	cr.metrics = rMetrics
	return nil
}

// RegisterRequest marks that we should expect to receive a reply from the given
// validator regarding the given chain and the reply should have the given
// requestID.
// The type of message we expect is [op].
// Every registered request must be cleared either by receiving a valid reply
// and passing it to the appropriate chain or by a timeout.
// This method registers a timeout that calls such methods if we don't get a
// reply in time.
func (cr *ChainRouter) RegisterRequest(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	op message.Op,
) {
	cr.lock.Lock()
	// When we receive a response message type (Chits, Put, Accepted, etc.)
	// we validate that we actually sent the corresponding request.
	// Give this request a unique ID so we can do that validation.
	uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID, op)
	// Add to the set of unfulfilled requests
	cr.timedRequests.Put(uniqueRequestID, requestEntry{
		time: cr.clock.Time(),
		op:   op,
	})
	cr.metrics.outstandingRequests.Set(float64(cr.timedRequests.Len()))
	cr.lock.Unlock()

	failedOp, exists := message.ResponseToFailedOps[op]
	if !exists {
		// This should never happen
		cr.log.Error("failed to convert operation type: %s", op)
		return
	}

	// Register a timeout to fire if we don't get a reply in time.
	cr.timeoutManager.RegisterRequest(nodeID, chainID, op, uniqueRequestID, func() {
		msg := cr.msgCreator.InternalFailedRequest(failedOp, nodeID, chainID, requestID)
		cr.HandleInbound(msg)
	})
}

func (cr *ChainRouter) HandleInbound(msg message.InboundMessage) {
	nodeID := msg.NodeID()
	op := msg.Op()
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	cr.log.AssertNoError(err)

	// AppGossip is the only message currently not containing a requestID
	// Here we assign the requestID already in use for gossiped containers
	// to allow a uniform handling of all messages
	var requestID uint32
	if op == message.AppGossip {
		requestID = constants.GossipMsgRequestID
	} else {
		requestID = msg.Get(message.RequestID).(uint32)
	}

	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists || !chain.IsValidator(nodeID) {
		cr.log.Debug(
			"Message %s from (%s. %s) dropped. Error: %s",
			op,
			nodeID,
			chainID,
			errUnknownChain,
		)

		msg.OnFinishedHandling()
		return
	}

	ctx := chain.Context()

	if _, notRequested := message.UnrequestedOps[op]; notRequested ||
		(op == message.Put && requestID == constants.GossipMsgRequestID) {
		if ctx.IsExecuting() {
			cr.log.Debug("dropping %s and skipping queue since the chain is currently executing", op)
			cr.metrics.droppedRequests.Inc()

			msg.OnFinishedHandling()
			return
		}
		chain.Push(msg)
		return
	}

	if expectedResponse, isFailed := message.FailedToResponseOps[op]; isFailed {
		// Create the request ID of the request we sent that this message is in
		// response to.
		uniqueRequestID, req := cr.clearRequest(expectedResponse, nodeID, chainID, requestID)
		if req == nil {
			// This was a duplicated response.
			msg.OnFinishedHandling()
			return
		}

		// Tell the timeout manager we are no longer expecting a response
		cr.timeoutManager.RemoveRequest(uniqueRequestID)

		// Pass the failure to the chain
		chain.Push(msg)
		return
	}

	if ctx.IsExecuting() {
		cr.log.Debug("dropping %s and skipping queue since the chain is currently executing", op)
		cr.metrics.droppedRequests.Inc()

		msg.OnFinishedHandling()
		return
	}

	uniqueRequestID, req := cr.clearRequest(op, nodeID, chainID, requestID)
	if req == nil {
		// We didn't request this message.
		msg.OnFinishedHandling()
		return
	}

	// Calculate how long it took [nodeID] to reply
	latency := cr.clock.Time().Sub(req.time)

	// Tell the timeout manager we got a response
	cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, req.op, latency)

	// Pass the response to the chain
	chain.Push(msg)
}

// Shutdown shuts down this router
func (cr *ChainRouter) Shutdown() {
	cr.log.Info("shutting down chain router")
	cr.lock.Lock()
	prevChains := cr.chains
	cr.chains = map[ids.ID]handler.Handler{}
	cr.lock.Unlock()

	for _, chain := range prevChains {
		chain.Stop()
	}

	ticker := time.NewTicker(cr.closeTimeout)
	defer ticker.Stop()

	for _, chain := range prevChains {
		select {
		case <-chain.Stopped():
		case <-ticker.C:
			cr.log.Warn("timed out while shutting down the chains")
			return
		}
	}
}

// AddChain registers the specified chain so that incoming
// messages can be routed to it
func (cr *ChainRouter) AddChain(chain handler.Handler) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	chainID := chain.Context().ChainID
	cr.log.Debug("registering chain %s with chain router", chainID)
	chain.SetOnStopped(func() {
		cr.removeChain(chainID)
	})
	cr.chains[chainID] = chain

	// Notify connected validators
	for validatorID, version := range cr.peers {
		// If this validator is benched on any chain, treat them as disconnected on all chains
		if _, benched := cr.benched[validatorID]; !benched {
			msg := cr.msgCreator.InternalConnected(validatorID, version)
			chain.Push(msg)
		}
	}
}

// Connected routes an incoming notification that a validator was just connected
func (cr *ChainRouter) Connected(validatorID ids.ShortID, nodeVersion version.Application) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	cr.peers[validatorID] = nodeVersion
	// If this validator is benched on any chain, treat them as disconnected on all chains
	if _, benched := cr.benched[validatorID]; benched {
		return
	}

	msg := cr.msgCreator.InternalConnected(validatorID, nodeVersion)

	// TODO: fire up an event when validator state changes i.e when they leave set, disconnect.
	// we cannot put a subnet-only validator check here since Disconnected would not be handled properly.
	for _, chain := range cr.chains {
		chain.Push(msg)
	}
}

// Disconnected routes an incoming notification that a validator was connected
func (cr *ChainRouter) Disconnected(validatorID ids.ShortID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	delete(cr.peers, validatorID)
	if _, benched := cr.benched[validatorID]; benched {
		return
	}

	msg := cr.msgCreator.InternalDisconnected(validatorID)

	// TODO: fire up an event when validator state changes i.e when they leave set, disconnect.
	// we cannot put a subnet-only validator check here since if a validator connects then it leaves validator-set, it would not be disconnected properly.
	for _, chain := range cr.chains {
		chain.Push(msg)
	}
}

// Benched routes an incoming notification that a validator was benched
func (cr *ChainRouter) Benched(chainID ids.ID, validatorID ids.ShortID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	benchedChains, exists := cr.benched[validatorID]
	benchedChains.Add(chainID)
	cr.benched[validatorID] = benchedChains
	_, hasPeer := cr.peers[validatorID]
	if exists || !hasPeer {
		// If the set already existed, then the node was previously benched.
		return
	}

	msg := cr.msgCreator.InternalDisconnected(validatorID)

	for _, chain := range cr.chains {
		chain.Push(msg)
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

	version, found := cr.peers[validatorID]
	if !found {
		return
	}

	msg := cr.msgCreator.InternalConnected(validatorID, version)

	for _, chain := range cr.chains {
		chain.Push(msg)
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

	chain.Stop()

	ticker := time.NewTicker(cr.closeTimeout)
	defer ticker.Stop()
	select {
	case <-chain.Stopped():
	case <-ticker.C:
		chain.Context().Log.Warn("timed out while shutting down")
	}

	if cr.onFatal != nil && cr.criticalChains.Contains(chainID) {
		go cr.onFatal(1)
	}
}

func (cr *ChainRouter) clearRequest(
	op message.Op,
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
) (ids.ID, *requestEntry) {
	// Create the request ID of the request we sent that this message is (allegedly) in response to.
	uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID, op)

	// Mark that an outstanding request has been fulfilled
	requestIntf, exists := cr.timedRequests.Get(uniqueRequestID)
	if !exists {
		return uniqueRequestID, nil
	}

	cr.timedRequests.Delete(uniqueRequestID)
	cr.metrics.outstandingRequests.Set(float64(cr.timedRequests.Len()))

	request := requestIntf.(requestEntry)
	return uniqueRequestID, &request
}

// Assumes [cr.lock] is held.
// Assumes [message.Op] is an alias of byte.
func (cr *ChainRouter) createRequestID(nodeID ids.ShortID, chainID ids.ID, requestID uint32, op message.Op) ids.ID {
	copy(cr.requestIDBytes, nodeID[:])
	copy(cr.requestIDBytes[hashing.AddrLen:], chainID[:])
	binary.BigEndian.PutUint32(cr.requestIDBytes[hashing.AddrLen+hashing.HashLen:], requestID)
	cr.requestIDBytes[hashing.AddrLen+hashing.HashLen+wrappers.IntLen] = byte(op)
	return hashing.ComputeHash256Array(cr.requestIDBytes)
}

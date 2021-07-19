// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	defaultCPUInterval = 15 * time.Second
)

var (
	errUnhealthy = errors.New("the router is not healthy")

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
	// Measures average rate at which messages are dropped
	dropRateCalculator math.Averager
	// Last time at which there were no outstanding requests
	lastTimeNoOutstanding time.Time
	// Used in [createRequestID].
	// Should only be accessed in that method.
	// [lock] should be held when [requestIDBytes] is accessed.
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
	// Set up meter to count dropped messages
	cr.dropRateCalculator = math.NewAverager(0, cr.healthConfig.MaxDropRateHalflife, cr.clock.Time())
	cr.healthConfig = healthConfig
	cr.requestIDBytes = make([]byte, 2*hashing.HashLen+wrappers.IntLen) // Validator ID, Chain ID, Request ID

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

// RegisterRequests marks that we should expect to receive a reply from the given validator
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
	uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID)
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

// GetAcceptedFrontier routes an incoming GetAcceptedFrontier request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (cr *ChainRouter) GetAcceptedFrontier(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		onFinishedHandling()
		cr.log.Debug("GetAcceptedFrontier(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
		return
	}

	// Pass the message to the chain
	chain.GetAcceptedFrontier(validatorID, requestID, deadline, onFinishedHandling)
}

// AcceptedFrontier routes an incoming AcceptedFrontier request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (cr *ChainRouter) AcceptedFrontier(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		onFinishedHandling()
		cr.log.Debug("AcceptedFrontier(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerIDs)
		return
	}

	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID)

	// Mark that an outstanding request has been fulfilled
	requestIntf, exists := cr.timedRequests.Get(uniqueRequestID)
	if !exists {
		onFinishedHandling()
		// We didn't request this message. Ignore.
		return
	}
	request := requestIntf.(requestEntry)
	if request.msgType != constants.GetAcceptedFrontierMsg {
		onFinishedHandling()
		// We got back a reply of wrong type. Ignore.
		return
	}
	cr.timedRequests.Delete(uniqueRequestID)

	// Calculate how long it took [validatorID] to reply
	latency := cr.clock.Time().Sub(request.time)

	// Tell the timeout manager we got a response
	cr.timeoutManager.RegisterResponse(validatorID, chainID, uniqueRequestID, constants.GetAcceptedFrontierMsg, latency)

	// Pass the response to the chain
	chain.AcceptedFrontier(validatorID, requestID, containerIDs, onFinishedHandling)
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

	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID)

	// Remove the outstanding request
	cr.removeRequest(uniqueRequestID)

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		// Should only happen if node is shutting down
		cr.log.Debug("GetAcceptedFrontierFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
		return
	}

	// Pass the response to the chain
	chain.GetAcceptedFrontierFailed(validatorID, requestID)
}

// GetAccepted routes an incoming GetAccepted request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (cr *ChainRouter) GetAccepted(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	containerIDs []ids.ID,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	chain, exists := cr.chains[chainID]
	if !exists {
		onFinishedHandling()
		cr.log.Debug("GetAccepted(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerIDs)
		return
	}

	// Pass the message to the chain.
	chain.GetAccepted(validatorID, requestID, deadline, containerIDs, onFinishedHandling)
}

// Accepted routes an incoming Accepted request from the validator with ID
// [validatorID] to the consensus engine working on the chain with ID
// [chainID]
func (cr *ChainRouter) Accepted(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		onFinishedHandling()
		cr.log.Debug("Accepted(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerIDs)
		return
	}

	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID)

	// Mark that an outstanding request has been fulfilled
	requestIntf, exists := cr.timedRequests.Get(uniqueRequestID)
	if !exists {
		// We didn't request this message. Ignore.
		onFinishedHandling()
		return
	}
	request := requestIntf.(requestEntry)
	if request.msgType != constants.GetAcceptedMsg {
		// We got back a reply of wrong type. Ignore.
		onFinishedHandling()
		return
	}
	cr.timedRequests.Delete(uniqueRequestID)

	// Calculate how long it took [validatorID] to reply
	latency := cr.clock.Time().Sub(request.time)

	// Tell the timeout manager we got a response
	cr.timeoutManager.RegisterResponse(validatorID, chainID, uniqueRequestID, constants.GetAcceptedMsg, latency)

	// Pass the response to the chain
	chain.Accepted(validatorID, requestID, containerIDs, onFinishedHandling)
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

	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID)

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

// GetAncestors routes an incoming GetAncestors message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
// The maximum number of ancestors to respond with is defined in snow/engine/commong/bootstrapper.go
func (cr *ChainRouter) GetAncestors(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		onFinishedHandling()
		cr.log.Debug("GetAncestors(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
		return
	}

	// Pass the message to the chain
	chain.GetAncestors(validatorID, requestID, deadline, containerID, onFinishedHandling)
}

// MultiPut routes an incoming MultiPut message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) MultiPut(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("MultiPut(%s, %s, %d, %d) dropped due to unknown chain", validatorID, chainID, requestID, len(containers))
		onFinishedHandling()
		return
	}

	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID)

	// Mark that an outstanding request has been fulfilled
	requestIntf, exists := cr.timedRequests.Get(uniqueRequestID)
	if !exists {
		// We didn't request this message. Ignore.
		onFinishedHandling()
		return
	}
	request := requestIntf.(requestEntry)
	if request.msgType != constants.GetAncestorsMsg {
		// We got back a reply of wrong type. Ignore.
		onFinishedHandling()
		return
	}
	cr.timedRequests.Delete(uniqueRequestID)

	// Calculate how long it took [validatorID] to reply
	latency := cr.clock.Time().Sub(request.time)

	// Tell the timeout manager we got a response
	cr.timeoutManager.RegisterResponse(validatorID, chainID, uniqueRequestID, constants.GetAncestorsMsg, latency)

	// Pass the response to the chain
	chain.MultiPut(validatorID, requestID, containers, onFinishedHandling)
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

	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID)

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

// Get routes an incoming Get request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) Get(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("Get(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
		onFinishedHandling()
		return
	}

	// Pass the message to the chain
	chain.Get(validatorID, requestID, deadline, containerID, onFinishedHandling)
}

// Put routes an incoming Put request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) Put(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	containerID ids.ID,
	container []byte,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		if requestID == constants.GossipMsgRequestID {
			cr.log.Verbo("Gossiped Put(%s, %s, %d, %s) dropped due to unknown chain. Container:",
				validatorID, chainID, requestID, containerID, formatting.DumpBytes{Bytes: container},
			)
		} else {
			cr.log.Debug("Put(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
			cr.log.Verbo("container:\n%s", formatting.DumpBytes{Bytes: container})
		}
		onFinishedHandling()
		return
	}

	// If this is a gossip message, pass to the chain
	if requestID == constants.GossipMsgRequestID {
		chain.Put(validatorID, requestID, containerID, container, onFinishedHandling)
		return
	}

	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID)

	// Mark that an outstanding request has been fulfilled
	requestIntf, exists := cr.timedRequests.Get(uniqueRequestID)
	if !exists {
		// We didn't request this message. Ignore.
		onFinishedHandling()
		return
	}
	request := requestIntf.(requestEntry)
	if request.msgType != constants.GetMsg {
		// We got back a reply of wrong type. Ignore.
		onFinishedHandling()
		return
	}
	cr.timedRequests.Delete(uniqueRequestID)

	// Calculate how long it took [validatorID] to reply
	latency := cr.clock.Time().Sub(request.time)

	// Tell the timeout manager we got a response
	cr.timeoutManager.RegisterResponse(validatorID, chainID, uniqueRequestID, constants.GetMsg, latency)

	// Pass the response to the chain
	chain.Put(validatorID, requestID, containerID, container, onFinishedHandling)
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

	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID)

	// Remove the outstanding request
	cr.removeRequest(uniqueRequestID)

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("GetFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
		return
	}

	// Pass the response to the chain
	chain.GetFailed(validatorID, requestID)
}

// PushQuery routes an incoming PushQuery request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) PushQuery(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	container []byte,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("PushQuery(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
		cr.log.Verbo("container:\n%s", formatting.DumpBytes{Bytes: container})
		onFinishedHandling()
		return
	}

	// Pass the message to the chain
	chain.PushQuery(validatorID, requestID, deadline, containerID, container, onFinishedHandling)
}

// PullQuery routes an incoming PullQuery request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) PullQuery(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("PullQuery(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
		onFinishedHandling()
		return
	}

	// Pass the message to the chain
	chain.PullQuery(validatorID, requestID, deadline, containerID, onFinishedHandling)
}

// Chits routes an incoming Chits message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) Chits(
	validatorID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	votes []ids.ID,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("Chits(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, votes)
		onFinishedHandling()
		return
	}

	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID)

	// Mark that an outstanding request has been fulfilled
	requestIntf, exists := cr.timedRequests.Get(uniqueRequestID)
	if !exists {
		// We didn't request this message. Ignore.
		onFinishedHandling()
		return
	}
	request := requestIntf.(requestEntry)
	if request.msgType != constants.PullQueryMsg && request.msgType != constants.PushQueryMsg {
		// We got back a reply of wrong type. Ignore.
		onFinishedHandling()
		return
	}
	cr.timedRequests.Delete(uniqueRequestID)

	// Calculate how long it took [validatorID] to reply
	latency := cr.clock.Time().Sub(request.time)

	// Tell the timeout manager we got a response
	cr.timeoutManager.RegisterResponse(validatorID, chainID, uniqueRequestID, request.msgType, latency)

	// Pass the response to the chain
	chain.Chits(validatorID, requestID, votes, onFinishedHandling)
}

// AppRequest routes an incoming application-level request from the given node
// to the consensus engine working on the given chain
func (cr *ChainRouter) AppRequest(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	appRequestBytes []byte,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("AppRequest(%s, %s, %d) dropped due to unknown chain", nodeID, chainID, requestID)
		cr.log.Verbo("dropped message: %s", formatting.DumpBytes{Bytes: appRequestBytes})
		onFinishedHandling()
		return
	}

	// Pass the message to the chain
	chain.AppRequest(nodeID, requestID, deadline, appRequestBytes, onFinishedHandling)
}

// AppResponse routes an incoming application-level response from the given node
// to the consensus engine working on the given chain
func (cr *ChainRouter) AppResponse(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	appResponseBytes []byte,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Get the chain, if it exists
	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("AppResponse(%s, %s, %d) dropped due to unknown chain", nodeID, chainID, requestID)
		cr.log.Verbo("dropped message: %s", formatting.DumpBytes{Bytes: appResponseBytes})
		onFinishedHandling()
		return
	}

	uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID)

	// Mark that an outstanding request has been fulfilled
	requestIntf, exists := cr.timedRequests.Get(uniqueRequestID)
	if !exists {
		// We didn't request this message. Ignore.
		onFinishedHandling()
		return
	}
	request := requestIntf.(requestEntry)
	if request.msgType != constants.AppRequestMsg {
		// We got back a reply of wrong type. Ignore.
		onFinishedHandling()
		return
	}
	cr.timedRequests.Delete(uniqueRequestID)

	// Calculate how long it took [nodeID] to reply
	latency := cr.clock.Time().Sub(request.time)

	// Tell the timeout manager we got a response
	cr.timeoutManager.RegisterResponse(nodeID, chainID, uniqueRequestID, request.msgType, latency)

	// Pass the response to the chain
	chain.AppResponse(nodeID, requestID, appResponseBytes, onFinishedHandling)
}

// AppGossip routes an incoming application-level gossip message from the given node
// to the consensus engine working on the given chain
func (cr *ChainRouter) AppGossip(
	nodeID ids.ShortID,
	chainID ids.ID,
	requestID uint32,
	appGossipBytes []byte,
	onFinishedHandling func(),
) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("AppGossip(%s, %s, %d) dropped due to unknown chain", nodeID, chainID, requestID)
		cr.log.Verbo("dropped message: %s", formatting.DumpBytes{Bytes: appGossipBytes})
		onFinishedHandling()
		return
	}

	// Pass the message to the chain
	chain.AppGossip(nodeID, requestID, appGossipBytes, onFinishedHandling)
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

	uniqueRequestID := cr.createRequestID(validatorID, chainID, requestID)

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

// AppRequestFailed notifies the given chain that it will not receive a response to its request
// with the given ID to the given node.
func (cr *ChainRouter) AppRequestFailed(nodeID ids.ShortID, chainID ids.ID, requestID uint32) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	uniqueRequestID := cr.createRequestID(nodeID, chainID, requestID)

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

// Connected routes an incoming notification that a validator was just connected
func (cr *ChainRouter) Connected(validatorID ids.ShortID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	cr.peers.Add(validatorID)
	// If this validator is benched on any chain, treat them as disconnected on all chains
	if _, benched := cr.benched[validatorID]; benched {
		return
	}

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

	dropRate := cr.dropRateCalculator.Read()
	healthy := dropRate <= cr.healthConfig.MaxDropRate
	details := map[string]interface{}{
		"msgDropRate": dropRate,
	}
	cr.metrics.msgDropRate.Set(dropRate)

	numOutstandingReqs := cr.timedRequests.Len()
	healthy = healthy && numOutstandingReqs <= cr.healthConfig.MaxOutstandingRequests
	details["outstandingRequests"] = numOutstandingReqs

	// check for long running requests
	now := cr.clock.Time()
	processingRequest := now
	if longestRunning, exists := cr.timedRequests.Oldest(); exists {
		processingRequest = longestRunning.(requestEntry).time
	}
	timeReqRunning := now.Sub(processingRequest)
	healthy = healthy && timeReqRunning <= cr.healthConfig.MaxOutstandingDuration
	details["longestRunningRequest"] = timeReqRunning.String()
	cr.metrics.longestRunningRequest.Set(float64(timeReqRunning.Milliseconds()))

	if !healthy {
		// The router is not healthy
		return details, errUnhealthy
	}
	return details, nil
}

// Assumes [cr.lock] is held
func (cr *ChainRouter) createRequestID(validatorID ids.ShortID, chainID ids.ID, requestID uint32) ids.ID {
	copy(cr.requestIDBytes, validatorID[:])
	copy(cr.requestIDBytes[hashing.HashLen:], chainID[:])
	binary.BigEndian.PutUint32(cr.requestIDBytes[2*hashing.HashLen:], requestID)
	return hashing.ComputeHash256Array(cr.requestIDBytes)
}

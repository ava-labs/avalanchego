// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errUnknownChain  = errors.New("received message for unknown chain")
	errUnallowedNode = errors.New("received message from non-allowed node")
	errClosing       = errors.New("router is closing")

	_ Router              = (*ChainRouter)(nil)
	_ benchlist.Benchable = (*ChainRouter)(nil)
)

type requestEntry struct {
	// When this request was registered
	time time.Time
	// The type of request that was made
	op message.Op
	// The engine type of the request that was made
	engineType p2p.EngineType
}

type peer struct {
	version *version.Application
	// The subnets that this peer is currently tracking
	trackedSubnets set.Set[ids.ID]
}

// ChainRouter routes incoming messages from the validator network
// to the consensus engines that the messages are intended for.
// Note that consensus engines are uniquely identified by the ID of the chain
// that they are working on.
// Invariant: P-chain must be registered before processing any messages
type ChainRouter struct {
	clock         mockable.Clock
	log           logging.Logger
	lock          sync.Mutex
	closing       bool
	chainHandlers map[ids.ID]handler.Handler

	// It is only safe to call [RegisterResponse] with the router lock held. Any
	// other calls to the timeout manager with the router lock held could cause
	// a deadlock because the timeout manager will call Benched and Unbenched.
	timeoutManager timeout.Manager

	closeTimeout time.Duration
	myNodeID     ids.NodeID
	peers        map[ids.NodeID]*peer
	// node ID --> chains that node is benched on
	// invariant: if a node is benched on any chain, it is treated as disconnected on all chains
	benched                map[ids.NodeID]set.Set[ids.ID]
	criticalChains         set.Set[ids.ID]
	sybilProtectionEnabled bool
	onFatal                func(exitCode int)
	metrics                *routerMetrics
	// Parameters for doing health checks
	healthConfig HealthConfig
	// aggregator of requests based on their time
	timedRequests *linked.Hashmap[ids.RequestID, requestEntry]
}

// Initialize the router.
//
// When this router receives an incoming message, it cancels the timeout in
// [timeouts] associated with the request that caused the incoming message, if
// applicable.
func (cr *ChainRouter) Initialize(
	nodeID ids.NodeID,
	log logging.Logger,
	timeoutManager timeout.Manager,
	closeTimeout time.Duration,
	criticalChains set.Set[ids.ID],
	sybilProtectionEnabled bool,
	trackedSubnets set.Set[ids.ID],
	onFatal func(exitCode int),
	healthConfig HealthConfig,
	reg prometheus.Registerer,
) error {
	cr.log = log
	cr.chainHandlers = make(map[ids.ID]handler.Handler)
	cr.timeoutManager = timeoutManager
	cr.closeTimeout = closeTimeout
	cr.benched = make(map[ids.NodeID]set.Set[ids.ID])
	cr.criticalChains = criticalChains
	cr.sybilProtectionEnabled = sybilProtectionEnabled
	cr.onFatal = onFatal
	cr.timedRequests = linked.NewHashmap[ids.RequestID, requestEntry]()
	cr.peers = make(map[ids.NodeID]*peer)
	cr.healthConfig = healthConfig

	// Mark myself as connected
	cr.myNodeID = nodeID
	myself := &peer{
		version: version.Current,
	}
	myself.trackedSubnets.Union(trackedSubnets)
	myself.trackedSubnets.Add(constants.PrimaryNetworkID)
	cr.peers[nodeID] = myself

	// Register metrics
	rMetrics, err := newRouterMetrics(reg)
	if err != nil {
		return err
	}
	cr.metrics = rMetrics
	return nil
}

// RegisterRequest marks that we should expect to receive a reply for a request
// from the given node's [chainID] and
// the reply should have the given requestID.
//
// The type of message we expect is [op].
//
// Every registered request must be cleared either by receiving a valid reply
// and passing it to the appropriate chain or by a timeout.
// This method registers a timeout that calls such methods if we don't get a
// reply in time.
func (cr *ChainRouter) RegisterRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
	op message.Op,
	timeoutMsg *message.InboundMessage,
	engineType p2p.EngineType,
) {
	cr.lock.Lock()
	if cr.closing {
		cr.log.Debug("dropping request",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", chainID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("messageOp", op),
			zap.Error(errClosing),
		)
		cr.lock.Unlock()
		return
	}
	// When we receive a response message type (Chits, Put, Accepted, etc.)
	// we validate that we actually sent the corresponding request.
	// Give this request a unique ID so we can do that validation.
	uniqueRequestID := ids.RequestID{
		NodeID:    nodeID,
		ChainID:   chainID,
		RequestID: requestID,
		Op:        byte(op),
	}
	// Add to the set of unfulfilled requests
	cr.timedRequests.Put(uniqueRequestID, requestEntry{
		time:       cr.clock.Time(),
		op:         op,
		engineType: engineType,
	})
	cr.metrics.outstandingRequests.Set(float64(cr.timedRequests.Len()))
	cr.lock.Unlock()

	// Determine whether we should include the latency of this request in our
	// measurements.
	// - Don't measure messages from ourself since these don't go over the
	//   network.
	// - Don't measure Puts because an adversary can cause us to issue a Get
	//   request to them and not respond, causing a timeout, skewing latency
	//   measurements.
	shouldMeasureLatency := nodeID != cr.myNodeID && op != message.PutOp

	// Register a timeout to fire if we don't get a reply in time.
	cr.timeoutManager.RegisterRequest(
		nodeID,
		chainID,
		shouldMeasureLatency,
		uniqueRequestID,
		func() {
			cr.handleMessage(ctx, timeoutMsg, true)
		},
	)
}

func (cr *ChainRouter) HandleInbound(ctx context.Context, msg *message.InboundMessage) {
	cr.handleMessage(ctx, msg, false)
}

func (cr *ChainRouter) HandleInternal(ctx context.Context, msg *message.InboundMessage) {
	// handleMessage is called in a separate goroutine because internal messages
	// may be sent while holding the chain's context lock. To enforce the
	// expected lock ordering, we must not grab the chain router lock while
	// holding the chain's context lock.
	go cr.handleMessage(ctx, msg, true)
}

// handleMessage routes a message to the specified chain. Messages may be
// unrequested, responses, or timeouts. The internal flag indicates whether the
// message is being sent from an internal component, such as due to a timeout,
// or if the message originated from a remote peer.
func (cr *ChainRouter) handleMessage(ctx context.Context, msg *message.InboundMessage, internal bool) {
	nodeID := msg.NodeID
	op := msg.Op

	m := msg.Message
	chainID, err := message.GetChainID(m)
	if err != nil {
		cr.log.Debug("dropping message with invalid field",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("messageOp", op),
			zap.String("field", "ChainID"),
			zap.Error(err),
		)

		msg.OnFinishedHandling()
		return
	}

	cr.lock.Lock()
	defer cr.lock.Unlock()

	if cr.closing {
		cr.log.Debug("dropping message",
			zap.Stringer("messageOp", op),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", chainID),
			zap.Error(errClosing),
		)
		msg.OnFinishedHandling()
		return
	}

	// Get the chain, if it exists
	chain, exists := cr.chainHandlers[chainID]
	if !exists {
		cr.log.Debug("dropping message",
			zap.Stringer("messageOp", op),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", chainID),
			zap.Error(errUnknownChain),
		)
		msg.OnFinishedHandling()
		return
	}

	if !internal && !chain.ShouldHandle(nodeID) {
		cr.log.Debug("dropping message",
			zap.Stringer("messageOp", op),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", chainID),
			zap.Error(errUnallowedNode),
		)
		msg.OnFinishedHandling()
		return
	}

	chainCtx := chain.Context()
	if message.UnrequestedOps.Contains(op) {
		if chainCtx.Executing.Get() {
			cr.log.Debug("dropping message and skipping queue",
				zap.String("reason", "the chain is currently executing"),
				zap.Stringer("messageOp", op),
			)
			cr.metrics.droppedRequests.Inc()
			msg.OnFinishedHandling()
			return
		}

		// Note: engineType is not guaranteed to be one of the explicitly named
		// enum values. If it was not specified it defaults to UNSPECIFIED.
		engineType, _ := message.GetEngineType(m)
		chain.Push(
			ctx,
			handler.Message{
				InboundMessage: msg,
				EngineType:     engineType,
			},
		)
		return
	}

	requestID, ok := message.GetRequestID(m)
	if !ok {
		cr.log.Debug("dropping message with invalid field",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("messageOp", op),
			zap.String("field", "RequestID"),
		)

		msg.OnFinishedHandling()
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
		chain.Push(
			ctx,
			handler.Message{
				InboundMessage: msg,
				EngineType:     req.engineType,
			},
		)
		return
	}

	if chainCtx.Executing.Get() {
		cr.log.Debug("dropping message and skipping queue",
			zap.String("reason", "the chain is currently executing"),
			zap.Stringer("messageOp", op),
		)
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
	chain.Push(
		ctx,
		handler.Message{
			InboundMessage: msg,
			EngineType:     req.engineType,
		},
	)
}

// Shutdown shuts down this router
func (cr *ChainRouter) Shutdown(ctx context.Context) {
	cr.log.Info("shutting down chain router")
	cr.lock.Lock()
	prevChains := cr.chainHandlers
	cr.chainHandlers = map[ids.ID]handler.Handler{}
	cr.closing = true
	cr.lock.Unlock()

	for _, chain := range prevChains {
		chain.Stop(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, cr.closeTimeout)
	defer cancel()

	for _, chain := range prevChains {
		shutdownDuration, err := chain.AwaitStopped(ctx)

		chainLog := chain.Context().Log
		if err != nil {
			chainLog.Warn("timed out while shutting down",
				zap.String("stack", utils.GetStacktrace(true)),
				zap.Error(err),
			)
		} else {
			chainLog.Info("chain shutdown",
				zap.Duration("shutdownDuration", shutdownDuration),
			)
		}
	}
}

// AddChain registers the specified chain so that incoming
// messages can be routed to it
func (cr *ChainRouter) AddChain(ctx context.Context, chain handler.Handler) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	chainID := chain.Context().ChainID
	if cr.closing {
		cr.log.Debug("dropping add chain request",
			zap.Stringer("chainID", chainID),
			zap.Error(errClosing),
		)
		return
	}
	cr.log.Debug("registering chain with chain router",
		zap.Stringer("chainID", chainID),
	)
	chain.SetOnStopped(func() {
		cr.removeChain(ctx, chainID)
	})
	cr.chainHandlers[chainID] = chain

	// Notify connected validators
	subnetID := chain.Context().SubnetID
	for validatorID, peer := range cr.peers {
		// If this validator is benched on any chain, treat them as disconnected
		// on all chains
		_, benched := cr.benched[validatorID]
		if benched {
			continue
		}

		// If this peer isn't running this chain, then we shouldn't mark them as
		// connected
		if !peer.trackedSubnets.Contains(subnetID) && cr.sybilProtectionEnabled {
			continue
		}

		msg := message.InternalConnected(validatorID, peer.version)
		chain.Push(ctx,
			handler.Message{
				InboundMessage: msg,
				EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
			},
		)
	}
}

// Connected routes an incoming notification that a validator was just connected
func (cr *ChainRouter) Connected(nodeID ids.NodeID, nodeVersion *version.Application, subnetID ids.ID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	if cr.closing {
		cr.log.Debug("dropping connected message",
			zap.Stringer("nodeID", nodeID),
			zap.Error(errClosing),
		)
		return
	}

	connectedPeer, exists := cr.peers[nodeID]
	if !exists {
		connectedPeer = &peer{
			version: nodeVersion,
		}
		cr.peers[nodeID] = connectedPeer
	}
	connectedPeer.trackedSubnets.Add(subnetID)

	// If this validator is benched on any chain, treat them as disconnected on all chains
	if _, benched := cr.benched[nodeID]; benched {
		return
	}

	msg := message.InternalConnected(nodeID, nodeVersion)

	// TODO: fire up an event when validator state changes i.e when they leave
	// set, disconnect. we cannot put an L1 validator check here since
	// Disconnected would not be handled properly.
	//
	// When sybil protection is disabled, we only want this clause to happen
	// once. Therefore, we only update the chains during the connection of the
	// primary network, which is guaranteed to happen for every peer.
	if cr.sybilProtectionEnabled || subnetID == constants.PrimaryNetworkID {
		for _, chain := range cr.chainHandlers {
			// If sybil protection is disabled, send a Connected message to
			// every chain when connecting to the primary network.
			if subnetID == chain.Context().SubnetID || !cr.sybilProtectionEnabled {
				chain.Push(
					context.TODO(),
					handler.Message{
						InboundMessage: msg,
						EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
					},
				)
			}
		}
	}
}

// Disconnected routes an incoming notification that a validator was connected
func (cr *ChainRouter) Disconnected(nodeID ids.NodeID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	if cr.closing {
		cr.log.Debug("dropping disconnected message",
			zap.Stringer("nodeID", nodeID),
			zap.Error(errClosing),
		)
		return
	}

	peer := cr.peers[nodeID]
	delete(cr.peers, nodeID)
	if _, benched := cr.benched[nodeID]; benched {
		return
	}

	msg := message.InternalDisconnected(nodeID)

	// TODO: fire up an event when validator state changes i.e when they leave
	// set, disconnect. we cannot put an L1 validator check here since
	// if a validator connects then it leaves validator-set, it would not be
	// disconnected properly.
	for _, chain := range cr.chainHandlers {
		if peer.trackedSubnets.Contains(chain.Context().SubnetID) || !cr.sybilProtectionEnabled {
			chain.Push(
				context.TODO(),
				handler.Message{
					InboundMessage: msg,
					EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
				})
		}
	}
}

// Benched routes an incoming notification that a validator was benched
func (cr *ChainRouter) Benched(chainID ids.ID, nodeID ids.NodeID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	if cr.closing {
		cr.log.Debug("dropping benched message",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", chainID),
			zap.Error(errClosing),
		)
		return
	}

	benchedChains, exists := cr.benched[nodeID]
	benchedChains.Add(chainID)
	cr.benched[nodeID] = benchedChains
	peer, hasPeer := cr.peers[nodeID]
	if exists || !hasPeer {
		// If the set already existed, then the node was previously benched.
		return
	}

	// This will disconnect the node from all subnets when issued to P-chain.
	// Even if there is no chain in the subnet.
	msg := message.InternalDisconnected(nodeID)

	for _, chain := range cr.chainHandlers {
		if peer.trackedSubnets.Contains(chain.Context().SubnetID) || !cr.sybilProtectionEnabled {
			chain.Push(
				context.TODO(),
				handler.Message{
					InboundMessage: msg,
					EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
				})
		}
	}
}

// Unbenched routes an incoming notification that a validator was just unbenched
func (cr *ChainRouter) Unbenched(chainID ids.ID, nodeID ids.NodeID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	if cr.closing {
		cr.log.Debug("dropping unbenched message",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", chainID),
			zap.Error(errClosing),
		)
		return
	}

	benchedChains := cr.benched[nodeID]
	benchedChains.Remove(chainID)
	if benchedChains.Len() != 0 {
		cr.benched[nodeID] = benchedChains
		return // This node is still benched
	}

	delete(cr.benched, nodeID)

	peer, found := cr.peers[nodeID]
	if !found {
		return
	}

	msg := message.InternalConnected(nodeID, peer.version)

	for _, chain := range cr.chainHandlers {
		if peer.trackedSubnets.Contains(chain.Context().SubnetID) || !cr.sybilProtectionEnabled {
			chain.Push(
				context.TODO(),
				handler.Message{
					InboundMessage: msg,
					EngineType:     p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
				})
		}
	}
}

// HealthCheck returns results of router health checks. Returns:
// 1) Information about health check results
// 2) An error if the health check reports unhealthy
func (cr *ChainRouter) HealthCheck(context.Context) (interface{}, error) {
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
		processingRequest = longestRunning.time
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
func (cr *ChainRouter) removeChain(ctx context.Context, chainID ids.ID) {
	cr.lock.Lock()
	chain, exists := cr.chainHandlers[chainID]
	if !exists {
		cr.log.Debug("can't remove unknown chain",
			zap.Stringer("chainID", chainID),
		)
		cr.lock.Unlock()
		return
	}
	delete(cr.chainHandlers, chainID)
	cr.lock.Unlock()

	chain.Stop(ctx)

	ctx, cancel := context.WithTimeout(ctx, cr.closeTimeout)
	shutdownDuration, err := chain.AwaitStopped(ctx)
	cancel()

	chainLog := chain.Context().Log
	if err != nil {
		chainLog.Warn("timed out while shutting down",
			zap.String("stack", utils.GetStacktrace(true)),
			zap.Error(err),
		)
	} else {
		chainLog.Info("chain shutdown",
			zap.Duration("shutdownDuration", shutdownDuration),
		)
	}

	if cr.onFatal != nil && cr.criticalChains.Contains(chainID) {
		go cr.onFatal(1)
	}
}

func (cr *ChainRouter) clearRequest(
	op message.Op,
	nodeID ids.NodeID,
	chainID ids.ID,
	requestID uint32,
) (ids.RequestID, *requestEntry) {
	// Create the request ID of the request we sent that this message is (allegedly) in response to.
	uniqueRequestID := ids.RequestID{
		NodeID:    nodeID,
		ChainID:   chainID,
		RequestID: requestID,
		Op:        byte(op),
	}
	// Mark that an outstanding request has been fulfilled
	request, exists := cr.timedRequests.Get(uniqueRequestID)
	if !exists {
		return uniqueRequestID, nil
	}

	cr.timedRequests.Delete(uniqueRequestID)
	cr.metrics.outstandingRequests.Set(float64(cr.timedRequests.Len()))
	return uniqueRequestID, &request
}

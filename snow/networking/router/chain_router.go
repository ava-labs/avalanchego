// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/handler"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errUnknownChain = errors.New("received message for unknown chain")

	_ Router              = (*ChainRouter)(nil)
	_ benchlist.Benchable = (*ChainRouter)(nil)
)

type requestEntry struct {
	// When this request was registered
	time time.Time
	// The type of request that was made
	op message.Op
}

type peer struct {
	version *version.Application
	// The subnets that this peer is currently tracking
	trackedSubnets set.Set[ids.ID]
	// The subnets that this peer actually has a connection to.
	// This is a subset of trackedSubnets.
	connectedSubnets set.Set[ids.ID]
}

// ChainRouter routes incoming messages from the validator network
// to the consensus engines that the messages are intended for.
// Note that consensus engines are uniquely identified by the ID of the chain
// that they are working on.
// Invariant: P-chain must be registered before processing any messages
type ChainRouter struct {
	clock  mockable.Clock
	log    logging.Logger
	lock   sync.Mutex
	chains map[ids.ID]handler.Handler

	// It is only safe to call [RegisterResponse] with the router lock held. Any
	// other calls to the timeout manager with the router lock held could cause
	// a deadlock because the timeout manager will call Benched and Unbenched.
	timeoutManager timeout.Manager

	closeTimeout time.Duration
	myNodeID     ids.NodeID
	peers        map[ids.NodeID]*peer
	// node ID --> chains that node is benched on
	// invariant: if a node is benched on any chain, it is treated as disconnected on all chains
	benched        map[ids.NodeID]set.Set[ids.ID]
	criticalChains set.Set[ids.ID]
	stakingEnabled bool
	onFatal        func(exitCode int)
	metrics        *routerMetrics
	// Parameters for doing health checks
	healthConfig HealthConfig
	// aggregator of requests based on their time
	timedRequests linkedhashmap.LinkedHashmap[ids.RequestID, requestEntry]
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
	stakingEnabled bool,
	trackedSubnets set.Set[ids.ID],
	onFatal func(exitCode int),
	healthConfig HealthConfig,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) error {
	cr.log = log
	cr.chains = make(map[ids.ID]handler.Handler)
	cr.timeoutManager = timeoutManager
	cr.closeTimeout = closeTimeout
	cr.benched = make(map[ids.NodeID]set.Set[ids.ID])
	cr.criticalChains = criticalChains
	cr.stakingEnabled = stakingEnabled
	cr.onFatal = onFatal
	cr.timedRequests = linkedhashmap.New[ids.RequestID, requestEntry]()
	cr.peers = make(map[ids.NodeID]*peer)
	cr.healthConfig = healthConfig

	// Mark myself as connected
	cr.myNodeID = nodeID
	myself := &peer{
		version: version.CurrentApp,
	}
	myself.trackedSubnets.Union(trackedSubnets)
	myself.trackedSubnets.Add(constants.PrimaryNetworkID)
	cr.peers[nodeID] = myself

	// Register metrics
	rMetrics, err := newRouterMetrics(metricsNamespace, metricsRegisterer)
	if err != nil {
		return err
	}
	cr.metrics = rMetrics
	return nil
}

// RegisterRequest marks that we should expect to receive a reply for a request
// issued by [requestingChainID] from the given node's [respondingChainID] and
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
	requestingChainID ids.ID,
	respondingChainID ids.ID,
	requestID uint32,
	op message.Op,
	failedMsg message.InboundMessage,
) {
	cr.lock.Lock()
	// When we receive a response message type (Chits, Put, Accepted, etc.)
	// we validate that we actually sent the corresponding request.
	// Give this request a unique ID so we can do that validation.
	//
	// For cross-chain messages, the responding chain is the source of the
	// response which is sent to the requester which is the destination,
	// which is why we flip the two in request id generation.
	uniqueRequestID := ids.RequestID{
		NodeID:             nodeID,
		SourceChainID:      respondingChainID,
		DestinationChainID: requestingChainID,
		RequestID:          requestID,
		Op:                 byte(op),
	}
	// Add to the set of unfulfilled requests
	cr.timedRequests.Put(uniqueRequestID, requestEntry{
		time: cr.clock.Time(),
		op:   op,
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
		respondingChainID,
		shouldMeasureLatency,
		uniqueRequestID,
		func() {
			cr.HandleInbound(ctx, failedMsg)
		},
	)
}

func (cr *ChainRouter) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	nodeID := msg.NodeID()
	op := msg.Op()

	m := msg.Message()
	destinationChainID, err := message.GetChainID(m)
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

	sourceChainID, err := message.GetSourceChainID(m)
	if err != nil {
		cr.log.Debug("dropping message with invalid field",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("messageOp", op),
			zap.String("field", "SourceChainID"),
			zap.Error(err),
		)

		msg.OnFinishedHandling()
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

	cr.lock.Lock()
	defer cr.lock.Unlock()

	// Get the chain, if it exists
	chain, exists := cr.chains[destinationChainID]
	if !exists || !chain.IsValidator(nodeID) {
		cr.log.Debug("dropping message",
			zap.Stringer("messageOp", op),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", destinationChainID),
			zap.Error(errUnknownChain),
		)
		msg.OnFinishedHandling()
		return
	}

	chainCtx := chain.Context()

	// TODO: [requestID] can overflow, which means a timeout on the request
	//       before the overflow may not be handled properly.
	if notRequested := message.UnrequestedOps.Contains(op); notRequested ||
		(op == message.PutOp && requestID == constants.GossipMsgRequestID) {
		if chainCtx.Executing.Get() {
			cr.log.Debug("dropping message and skipping queue",
				zap.String("reason", "the chain is currently executing"),
				zap.Stringer("messageOp", op),
			)
			cr.metrics.droppedRequests.Inc()
			msg.OnFinishedHandling()
			return
		}
		chain.Push(ctx, msg)
		return
	}

	if expectedResponse, isFailed := message.FailedToResponseOps[op]; isFailed {
		// Create the request ID of the request we sent that this message is in
		// response to.
		uniqueRequestID, req := cr.clearRequest(expectedResponse, nodeID, sourceChainID, destinationChainID, requestID)
		if req == nil {
			// This was a duplicated response.
			msg.OnFinishedHandling()
			return
		}

		// Tell the timeout manager we are no longer expecting a response
		cr.timeoutManager.RemoveRequest(uniqueRequestID)

		// Pass the failure to the chain
		chain.Push(ctx, msg)
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

	uniqueRequestID, req := cr.clearRequest(op, nodeID, sourceChainID, destinationChainID, requestID)
	if req == nil {
		// We didn't request this message.
		msg.OnFinishedHandling()
		return
	}

	// Calculate how long it took [nodeID] to reply
	latency := cr.clock.Time().Sub(req.time)

	// Tell the timeout manager we got a response
	cr.timeoutManager.RegisterResponse(nodeID, destinationChainID, uniqueRequestID, req.op, latency)

	// Pass the response to the chain
	chain.Push(ctx, msg)
}

// Shutdown shuts down this router
func (cr *ChainRouter) Shutdown(ctx context.Context) {
	cr.log.Info("shutting down chain router")
	cr.lock.Lock()
	prevChains := cr.chains
	cr.chains = map[ids.ID]handler.Handler{}
	cr.lock.Unlock()

	for _, chain := range prevChains {
		chain.Stop(ctx)
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
func (cr *ChainRouter) AddChain(ctx context.Context, chain handler.Handler) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	chainID := chain.Context().ChainID
	cr.log.Debug("registering chain with chain router",
		zap.Stringer("chainID", chainID),
	)
	chain.SetOnStopped(func() {
		cr.removeChain(ctx, chainID)
	})
	cr.chains[chainID] = chain

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
		if !peer.trackedSubnets.Contains(subnetID) && cr.stakingEnabled {
			continue
		}

		msg := message.InternalConnected(validatorID, peer.version)
		chain.Push(ctx, msg)
	}

	// When we register the P-chain, we mark ourselves as connected on all of
	// the subnets that we have tracked.
	if chainID != constants.PlatformChainID {
		return
	}

	// If we have currently benched ourselves, we will mark ourselves as
	// connected when we unbench. So skip connecting now.
	// This is not "theoretically" possible, but keeping this here prevents us
	// from keeping an invariant that we never bench ourselves.
	if _, benched := cr.benched[cr.myNodeID]; benched {
		return
	}

	myself := cr.peers[cr.myNodeID]
	for subnetID := range myself.trackedSubnets {
		cr.connectedSubnet(myself, cr.myNodeID, subnetID)
	}
}

// Connected routes an incoming notification that a validator was just connected
func (cr *ChainRouter) Connected(nodeID ids.NodeID, nodeVersion *version.Application, subnetID ids.ID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

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
	// set, disconnect. we cannot put a subnet-only validator check here since
	// Disconnected would not be handled properly.
	//
	// When staking is disabled, we only want this clause to happen once.
	// Therefore, we only update the chains during the connection of the primary
	// network, which is guaranteed to happen for every peer.
	if cr.stakingEnabled || subnetID == constants.PrimaryNetworkID {
		for _, chain := range cr.chains {
			// If staking is disabled, send a Connected message to every chain
			// when connecting to the primary network
			if subnetID == chain.Context().SubnetID || !cr.stakingEnabled {
				chain.Push(context.TODO(), msg)
			}
		}
	}

	cr.connectedSubnet(connectedPeer, nodeID, subnetID)
}

// Disconnected routes an incoming notification that a validator was connected
func (cr *ChainRouter) Disconnected(nodeID ids.NodeID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	peer := cr.peers[nodeID]
	delete(cr.peers, nodeID)
	if _, benched := cr.benched[nodeID]; benched {
		return
	}

	msg := message.InternalDisconnected(nodeID)

	// TODO: fire up an event when validator state changes i.e when they leave
	// set, disconnect. we cannot put a subnet-only validator check here since
	// if a validator connects then it leaves validator-set, it would not be
	// disconnected properly.
	for _, chain := range cr.chains {
		if peer.trackedSubnets.Contains(chain.Context().SubnetID) || !cr.stakingEnabled {
			chain.Push(context.TODO(), msg)
		}
	}
}

// Benched routes an incoming notification that a validator was benched
func (cr *ChainRouter) Benched(chainID ids.ID, nodeID ids.NodeID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

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

	for _, chain := range cr.chains {
		if peer.trackedSubnets.Contains(chain.Context().SubnetID) || !cr.stakingEnabled {
			chain.Push(context.TODO(), msg)
		}
	}

	peer.connectedSubnets.Clear()
}

// Unbenched routes an incoming notification that a validator was just unbenched
func (cr *ChainRouter) Unbenched(chainID ids.ID, nodeID ids.NodeID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

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

	for _, chain := range cr.chains {
		if peer.trackedSubnets.Contains(chain.Context().SubnetID) || !cr.stakingEnabled {
			chain.Push(context.TODO(), msg)
		}
	}

	// This will unbench the node from all its subnets.
	// We handle this case separately because the node may have been benched on
	// a subnet that has no chains.
	for subnetID := range peer.trackedSubnets {
		cr.connectedSubnet(peer, nodeID, subnetID)
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
	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("can't remove unknown chain",
			zap.Stringer("chainID", chainID),
		)
		cr.lock.Unlock()
		return
	}
	delete(cr.chains, chainID)
	cr.lock.Unlock()

	chain.Stop(ctx)

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
	nodeID ids.NodeID,
	sourceChainID ids.ID,
	destinationChainID ids.ID,
	requestID uint32,
) (ids.RequestID, *requestEntry) {
	// Create the request ID of the request we sent that this message is (allegedly) in response to.
	uniqueRequestID := ids.RequestID{
		NodeID:             nodeID,
		SourceChainID:      sourceChainID,
		DestinationChainID: destinationChainID,
		RequestID:          requestID,
		Op:                 byte(op),
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

// connectedSubnet pushes an InternalSubnetConnected message with [nodeID] and
// [subnetID] to the P-chain. This should be called when a node is either first
// connecting to [subnetID] or when a node that was already connected is
// unbenched on [subnetID]. This is a noop if [subnetID] is the Primary Network
// or if the peer is already marked as connected to the subnet.
// Invariant: should be called after *message.Connected is pushed to the P-chain
// Invariant: should be called after the P-chain was provided in [AddChain]
func (cr *ChainRouter) connectedSubnet(peer *peer, nodeID ids.NodeID, subnetID ids.ID) {
	// if connected to primary network, we can skip this
	// because Connected has its own internal message
	if subnetID == constants.PrimaryNetworkID {
		return
	}

	// peer already connected to this subnet
	if peer.connectedSubnets.Contains(subnetID) {
		return
	}

	msg := message.InternalConnectedSubnet(nodeID, subnetID)
	// We only push this message to the P-chain because it is the only chain
	// that cares about the connectivity of all subnets. Others chains learn
	// about the connectivity of their own subnet when they receive a
	// *message.Connected.
	platformChain, ok := cr.chains[constants.PlatformChainID]
	if !ok {
		cr.log.Error("trying to issue InternalConnectedSubnet message, but platform chain is not registered",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("subnetID", subnetID),
		)
		return
	}
	platformChain.Push(context.TODO(), msg)

	peer.connectedSubnets.Add(subnetID)
}

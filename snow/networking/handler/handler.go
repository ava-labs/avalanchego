// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/networking/worker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
)

const (
	threadPoolSize        = 2
	numDispatchersToClose = 3
)

var _ Handler = (*handler)(nil)

type Handler interface {
	common.Timer
	health.Checker

	Context() *snow.ConsensusContext
	IsValidator(nodeID ids.NodeID) bool

	SetStateSyncer(engine common.StateSyncer)
	StateSyncer() common.StateSyncer
	SetBootstrapper(engine common.BootstrapableEngine)
	Bootstrapper() common.BootstrapableEngine
	SetConsensus(engine common.Engine)
	Consensus() common.Engine

	SetOnStopped(onStopped func())
	Start(recoverPanic bool)
	Push(ctx context.Context, msg message.InboundMessage)
	Len() int
	Stop()
	StopWithError(err error)
	Stopped() chan struct{}
}

// handler passes incoming messages from the network to the consensus engine.
// (Actually, it receives the incoming messages from a ChainRouter, but same difference.)
type handler struct {
	metrics *metrics

	// Useful for faking time in tests
	clock mockable.Clock

	ctx *snow.ConsensusContext
	mc  message.InternalMsgBuilder
	// The validator set that validates this chain
	validators validators.Set
	// Receives messages from the VM
	msgFromVMChan   <-chan common.Message
	preemptTimeouts chan struct{}
	gossipFrequency time.Duration

	stateSyncer  common.StateSyncer
	bootstrapper common.BootstrapableEngine
	engine       common.Engine
	// onStopped is called in a goroutine when this handler finishes shutting
	// down. If it is nil then it is skipped.
	onStopped func()

	// Tracks cpu/disk usage caused by each peer.
	resourceTracker tracker.ResourceTracker

	// Holds messages that [engine] hasn't processed yet.
	// [unprocessedMsgsCond.L] must be held while accessing [syncMessageQueue].
	syncMessageQueue MessageQueue
	// Holds messages that [engine] hasn't processed yet.
	// [unprocessedAsyncMsgsCond.L] must be held while accessing [asyncMessageQueue].
	asyncMessageQueue MessageQueue
	// Worker pool for handling asynchronous consensus messages
	asyncMessagePool worker.Pool
	timeouts         chan struct{}

	closeOnce            sync.Once
	closingChan          chan struct{}
	numDispatchersClosed int
	// Closed when this handler and [engine] are done shutting down
	closed chan struct{}
}

// Initialize this consensus handler
// [engine] must be initialized before initializing this handler
func New(
	mc message.InternalMsgBuilder,
	ctx *snow.ConsensusContext,
	validators validators.Set,
	msgFromVMChan <-chan common.Message,
	preemptTimeouts chan struct{},
	gossipFrequency time.Duration,
	resourceTracker tracker.ResourceTracker,
) (Handler, error) {
	h := &handler{
		ctx:              ctx,
		mc:               mc,
		validators:       validators,
		msgFromVMChan:    msgFromVMChan,
		preemptTimeouts:  preemptTimeouts,
		gossipFrequency:  gossipFrequency,
		asyncMessagePool: worker.NewPool(threadPoolSize),
		timeouts:         make(chan struct{}, 1),
		closingChan:      make(chan struct{}),
		closed:           make(chan struct{}),
		resourceTracker:  resourceTracker,
	}

	var err error

	h.metrics, err = newMetrics("handler", h.ctx.Registerer)
	if err != nil {
		return nil, fmt.Errorf("initializing handler metrics errored with: %w", err)
	}
	cpuTracker := resourceTracker.CPUTracker()
	h.syncMessageQueue, err = NewMessageQueue(h.ctx.Log, h.validators, cpuTracker, "handler", h.ctx.Registerer, message.SynchronousOps)
	if err != nil {
		return nil, fmt.Errorf("initializing sync message queue errored with: %w", err)
	}
	h.asyncMessageQueue, err = NewMessageQueue(h.ctx.Log, h.validators, cpuTracker, "handler_async", h.ctx.Registerer, message.AsynchronousOps)
	if err != nil {
		return nil, fmt.Errorf("initializing async message queue errored with: %w", err)
	}
	return h, nil
}

func (h *handler) Context() *snow.ConsensusContext { return h.ctx }

func (h *handler) IsValidator(nodeID ids.NodeID) bool {
	return !h.ctx.IsValidatorOnly() ||
		nodeID == h.ctx.NodeID ||
		h.validators.Contains(nodeID)
}

func (h *handler) SetStateSyncer(engine common.StateSyncer) { h.stateSyncer = engine }
func (h *handler) StateSyncer() common.StateSyncer          { return h.stateSyncer }

func (h *handler) SetBootstrapper(engine common.BootstrapableEngine) { h.bootstrapper = engine }
func (h *handler) Bootstrapper() common.BootstrapableEngine          { return h.bootstrapper }

func (h *handler) SetConsensus(engine common.Engine) { h.engine = engine }
func (h *handler) Consensus() common.Engine          { return h.engine }

func (h *handler) SetOnStopped(onStopped func()) { h.onStopped = onStopped }

func (h *handler) selectStartingGear() (common.Engine, error) {
	if h.stateSyncer == nil {
		return h.bootstrapper, nil
	}

	stateSyncEnabled, err := h.stateSyncer.IsEnabled()
	if err != nil {
		return nil, err
	}

	if !stateSyncEnabled {
		return h.bootstrapper, nil
	}

	// drop bootstrap state from previous runs
	// before starting state sync
	return h.stateSyncer, h.bootstrapper.Clear()
}

func (h *handler) Start(recoverPanic bool) {
	h.ctx.Lock.Lock()
	defer h.ctx.Lock.Unlock()

	gear, err := h.selectStartingGear()
	if err != nil {
		h.ctx.Log.Error("chain failed to select starting gear",
			zap.Error(err),
		)
		h.shutdown()
		return
	}

	if err := gear.Start(0); err != nil {
		h.ctx.Log.Error("chain failed to start",
			zap.Error(err),
		)
		h.shutdown()
		return
	}

	if recoverPanic {
		go h.ctx.Log.RecoverAndExit(h.dispatchSync, func() {
			h.ctx.Log.Error("chain was shutdown due to a panic in the sync dispatcher")
		})
		go h.ctx.Log.RecoverAndExit(h.dispatchAsync, func() {
			h.ctx.Log.Error("chain was shutdown due to a panic in the async dispatcher")
		})
		go h.ctx.Log.RecoverAndExit(h.dispatchChans, func() {
			h.ctx.Log.Error("chain was shutdown due to a panic in the chan dispatcher")
		})
	} else {
		go h.ctx.Log.RecoverAndPanic(h.dispatchSync)
		go h.ctx.Log.RecoverAndPanic(h.dispatchAsync)
		go h.ctx.Log.RecoverAndPanic(h.dispatchChans)
	}
}

func (h *handler) HealthCheck() (interface{}, error) {
	h.ctx.Lock.Lock()
	defer h.ctx.Lock.Unlock()

	engine, err := h.getEngine()
	if err != nil {
		return nil, err
	}
	return engine.HealthCheck()
}

// Push the message onto the handler's queue
func (h *handler) Push(ctx context.Context, msg message.InboundMessage) {
	switch msg.Op() {
	case message.AppRequest, message.AppGossip, message.AppRequestFailed, message.AppResponse,
		message.CrossChainAppRequest, message.CrossChainAppRequestFailed, message.CrossChainAppResponse:
		h.asyncMessageQueue.Push(ctx, msg)
	default:
		h.syncMessageQueue.Push(ctx, msg)
	}
}

func (h *handler) Len() int {
	return h.syncMessageQueue.Len() + h.asyncMessageQueue.Len()
}

func (h *handler) RegisterTimeout(d time.Duration) {
	go func() {
		timer := time.NewTimer(d)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-h.preemptTimeouts:
		}

		// If there is already a timeout ready to fire - just drop the
		// additional timeout. This ensures that all goroutines that are spawned
		// here are able to close if the chain is shutdown.
		select {
		case h.timeouts <- struct{}{}:
		default:
		}
	}()
}

func (h *handler) Stop() {
	h.closeOnce.Do(func() {
		// Must hold the locks here to ensure there's no race condition in where
		// we check the value of [h.closing] after the call to [Signal].
		h.syncMessageQueue.Shutdown()
		h.asyncMessageQueue.Shutdown()
		close(h.closingChan)

		// TODO: switch this to use a [context.Context] with a cancel function.
		//
		// Don't process any more bootstrap messages. If a dispatcher is
		// processing a bootstrap message, stop. We do this because if we
		// didn't, and the engine was in the middle of executing state
		// transitions during bootstrapping, we wouldn't be able to grab
		// [h.ctx.Lock] until the engine finished executing state transitions,
		// which may take a long time. As a result, the router would time out on
		// shutting down this chain.
		h.bootstrapper.Halt()
	})
}

func (h *handler) StopWithError(err error) {
	h.ctx.Log.Fatal("shutting down chain",
		zap.String("reason", "received an unexpected error"),
		zap.Error(err),
	)
	h.Stop()
}

func (h *handler) Stopped() chan struct{} { return h.closed }

func (h *handler) dispatchSync() {
	defer h.closeDispatcher()

	// Handle sync messages from the router
	for {
		// Get the next message we should process. If the handler is shutting
		// down, we may fail to pop a message.
		ctx, msg, ok := h.popUnexpiredMsg(h.syncMessageQueue, h.metrics.expired)
		if !ok {
			return
		}

		// If there is an error handling the message, shut down the chain
		if err := h.handleSyncMsg(ctx, msg); err != nil {
			h.StopWithError(fmt.Errorf(
				"%w while processing sync message: %s",
				err,
				msg,
			))
			return
		}
	}
}

func (h *handler) dispatchAsync() {
	defer func() {
		h.asyncMessagePool.Shutdown()
		h.closeDispatcher()
	}()

	// Handle async messages from the router
	for {
		// Get the next message we should process. If the handler is shutting
		// down, we may fail to pop a message.
		ctx, msg, ok := h.popUnexpiredMsg(h.asyncMessageQueue, h.metrics.asyncExpired)
		if !ok {
			return
		}

		h.handleAsyncMsg(ctx, msg)
	}
}

func (h *handler) dispatchChans() {
	gossiper := time.NewTicker(h.gossipFrequency)
	defer func() {
		gossiper.Stop()
		h.closeDispatcher()
	}()

	// Handle messages generated by the handler and the VM
	for {
		var msg message.InboundMessage
		select {
		case <-h.closingChan:
			return

		case vmMSG := <-h.msgFromVMChan:
			msg = h.mc.InternalVMMessage(h.ctx.NodeID, uint32(vmMSG))

		case <-gossiper.C:
			msg = h.mc.InternalGossipRequest(h.ctx.NodeID)

		case <-h.timeouts:
			msg = h.mc.InternalTimeout(h.ctx.NodeID)
		}

		if err := h.handleChanMsg(msg); err != nil {
			h.StopWithError(fmt.Errorf(
				"%w while processing async message: %s",
				err,
				msg,
			))
			return
		}
	}
}

// Any returned error is treated as fatal
func (h *handler) handleSyncMsg(ctx context.Context, msg message.InboundMessage) error {
	h.ctx.Log.Debug("forwarding sync message to consensus",
		zap.Stringer("messageString", msg),
	)

	var (
		nodeID    = msg.NodeID()
		op        = msg.Op()
		startTime = h.clock.Time()
	)
	h.resourceTracker.StartProcessing(nodeID, startTime)
	h.ctx.Lock.Lock()
	defer func() {
		h.ctx.Lock.Unlock()

		var (
			endTime   = h.clock.Time()
			histogram = h.metrics.messages[op]
		)
		h.resourceTracker.StopProcessing(nodeID, endTime)
		histogram.Observe(float64(endTime.Sub(startTime)))
		msg.OnFinishedHandling()
		h.ctx.Log.Debug("finished handling sync message",
			zap.Stringer("messageOp", op),
		)
	}()

	engine, err := h.getEngine()
	if err != nil {
		return err
	}

	// Invariant: msg.Get(message.RequestID) must never error. The [ChainRouter]
	//            should have already successfully called this function.
	// Invariant: Response messages can never be dropped here. This is because
	//            the timeout has already been cleared. This means the engine
	//            should be invoked with a failure message if parsing of the
	//            response fails.
	switch op {
	case message.GetStateSummaryFrontier:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		return engine.GetStateSummaryFrontier(ctx, nodeID, requestID)

	case message.StateSummaryFrontier:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		summaryIntf, err := msg.Get(message.SummaryBytes)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.SummaryBytes),
				zap.Error(err),
			)
			return engine.GetStateSummaryFrontierFailed(ctx, nodeID, requestID)
		}
		summary := summaryIntf.([]byte)

		return engine.StateSummaryFrontier(ctx, nodeID, requestID, summary)

	case message.GetStateSummaryFrontierFailed:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		return engine.GetStateSummaryFrontierFailed(ctx, nodeID, requestID)

	case message.GetAcceptedStateSummary:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		summaryHeights, err := getSummaryHeights(msg)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.SummaryHeights),
				zap.Error(err),
			)
			return nil
		}

		return engine.GetAcceptedStateSummary(ctx, nodeID, requestID, summaryHeights)

	case message.AcceptedStateSummary:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		summaryIDs, err := getIDs(message.SummaryIDs, msg)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.SummaryIDs),
				zap.Error(err),
			)
			return engine.GetAcceptedStateSummaryFailed(ctx, nodeID, requestID)
		}

		return engine.AcceptedStateSummary(ctx, nodeID, requestID, summaryIDs)

	case message.GetAcceptedStateSummaryFailed:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		return engine.GetAcceptedStateSummaryFailed(ctx, nodeID, requestID)

	case message.GetAcceptedFrontier:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		return engine.GetAcceptedFrontier(ctx, nodeID, requestID)

	case message.AcceptedFrontier:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		containerIDs, err := getIDs(message.ContainerIDs, msg)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerIDs),
				zap.Error(err),
			)
			return engine.GetAcceptedFrontierFailed(ctx, nodeID, requestID)
		}

		return engine.AcceptedFrontier(ctx, nodeID, requestID, containerIDs)

	case message.GetAcceptedFrontierFailed:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		return engine.GetAcceptedFrontierFailed(ctx, nodeID, requestID)

	case message.GetAccepted:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		containerIDs, err := getIDs(message.ContainerIDs, msg)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerIDs),
				zap.Error(err),
			)
			return nil
		}

		return engine.GetAccepted(ctx, nodeID, requestID, containerIDs)

	case message.Accepted:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		containerIDs, err := getIDs(message.ContainerIDs, msg)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerIDs),
				zap.Error(err),
			)
			return engine.GetAcceptedFailed(ctx, nodeID, requestID)
		}

		return engine.Accepted(ctx, nodeID, requestID, containerIDs)

	case message.GetAcceptedFailed:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		return engine.GetAcceptedFailed(ctx, nodeID, requestID)

	case message.GetAncestors:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		containerIDIntf, err := msg.Get(message.ContainerID)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerID),
				zap.Error(err),
			)
			return nil
		}
		containerIDBytes := containerIDIntf.([]byte)
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerID),
				zap.Error(err),
			)
			return nil
		}

		return engine.GetAncestors(ctx, nodeID, requestID, containerID)

	case message.GetAncestorsFailed:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		return engine.GetAncestorsFailed(ctx, nodeID, requestID)

	case message.Ancestors:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		containersIntf, err := msg.Get(message.MultiContainerBytes)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.MultiContainerBytes),
				zap.Error(err),
			)
			return engine.GetAncestorsFailed(ctx, nodeID, requestID)
		}
		containers := containersIntf.([][]byte)

		return engine.Ancestors(ctx, nodeID, requestID, containers)

	case message.Get:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		containerIDIntf, err := msg.Get(message.ContainerID)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerID),
				zap.Error(err),
			)
			return nil
		}
		containerIDBytes := containerIDIntf.([]byte)
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerID),
				zap.Error(err),
			)
			return nil
		}

		return engine.Get(ctx, nodeID, requestID, containerID)

	case message.GetFailed:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		return engine.GetFailed(ctx, nodeID, requestID)

	case message.Put:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		containerIntf, err := msg.Get(message.ContainerBytes)
		if err != nil {
			// TODO: [requestID] can overflow, which means a timeout on the
			//       request before the overflow may not be handled properly.
			if requestID == constants.GossipMsgRequestID {
				h.ctx.Log.Debug("dropping message with invalid field",
					zap.Stringer("nodeID", nodeID),
					zap.Stringer("messageOp", op),
					zap.Uint32("requestID", requestID),
					zap.Stringer("field", message.ContainerBytes),
					zap.Error(err),
				)
				return nil
			}

			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerBytes),
				zap.Error(err),
			)
			return engine.GetFailed(ctx, nodeID, requestID)
		}
		container := containerIntf.([]byte)

		return engine.Put(ctx, nodeID, requestID, container)

	case message.PushQuery:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		containerIntf, err := msg.Get(message.ContainerBytes)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerBytes),
				zap.Error(err),
			)
			return nil
		}
		container := containerIntf.([]byte)

		return engine.PushQuery(ctx, nodeID, requestID, container)

	case message.PullQuery:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		containerIDIntf, err := msg.Get(message.ContainerID)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerID),
				zap.Error(err),
			)
			return nil
		}
		containerIDBytes := containerIDIntf.([]byte)
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerID),
				zap.Error(err),
			)
			return nil
		}

		return engine.PullQuery(ctx, nodeID, requestID, containerID)

	case message.Chits:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		votes, err := getIDs(message.ContainerIDs, msg)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.ContainerIDs),
				zap.Error(err),
			)
			return engine.QueryFailed(ctx, nodeID, requestID)
		}

		return engine.Chits(ctx, nodeID, requestID, votes)

	case message.QueryFailed:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		return engine.QueryFailed(ctx, nodeID, requestID)

	case message.Connected:
		peerVersionIntf, err := msg.Get(message.VersionStruct)
		if err != nil {
			return err
		}
		peerVersion := peerVersionIntf.(*version.Application)

		return engine.Connected(nodeID, peerVersion)

	case message.Disconnected:
		return engine.Disconnected(nodeID)

	default:
		return fmt.Errorf(
			"attempt to submit unhandled sync msg %s from %s",
			op, nodeID,
		)
	}
}

func (h *handler) handleAsyncMsg(ctx context.Context, msg message.InboundMessage) {
	h.asyncMessagePool.Send(func() {
		if err := h.executeAsyncMsg(ctx, msg); err != nil {
			h.StopWithError(fmt.Errorf(
				"%w while processing async message: %s",
				err,
				msg,
			))
		}
	})
}

// Any returned error is treated as fatal
func (h *handler) executeAsyncMsg(ctx context.Context, msg message.InboundMessage) error {
	h.ctx.Log.Debug("forwarding async message to consensus",
		zap.Stringer("messageString", msg),
	)

	var (
		nodeID    = msg.NodeID()
		op        = msg.Op()
		startTime = h.clock.Time()
	)
	h.resourceTracker.StartProcessing(nodeID, startTime)
	defer func() {
		var (
			endTime   = h.clock.Time()
			histogram = h.metrics.messages[op]
		)
		h.resourceTracker.StopProcessing(nodeID, endTime)
		histogram.Observe(float64(endTime.Sub(startTime)))
		msg.OnFinishedHandling()
		h.ctx.Log.Debug("finished handling async message",
			zap.Stringer("messageOp", op),
		)
	}()

	engine, err := h.getEngine()
	if err != nil {
		return err
	}

	switch op {
	case message.AppRequest:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		appBytesIntf, err := msg.Get(message.AppBytes)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.AppBytes),
				zap.Error(err),
			)
			return nil
		}
		appBytes := appBytesIntf.([]byte)

		return engine.AppRequest(ctx, nodeID, requestID, msg.ExpirationTime(), appBytes)

	case message.AppResponse:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		appBytesIntf, err := msg.Get(message.AppBytes)
		if err != nil {
			h.ctx.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.AppBytes),
				zap.Error(err),
			)
			return engine.AppRequestFailed(ctx, nodeID, requestID)
		}
		appBytes := appBytesIntf.([]byte)

		return engine.AppResponse(ctx, nodeID, requestID, appBytes)

	case message.AppRequestFailed:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)

		return engine.AppRequestFailed(ctx, nodeID, requestID)

	case message.AppGossip:
		appBytesIntf, err := msg.Get(message.AppBytes)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Stringer("field", message.AppBytes),
				zap.Error(err),
			)
			return nil
		}
		appBytes := appBytesIntf.([]byte)

		return engine.AppGossip(ctx, nodeID, appBytes)

	case message.CrossChainAppRequest:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)
		sourceChainIDIntf, err := msg.Get(message.SourceChainID)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Stringer("field", message.SourceChainID),
				zap.Error(err),
			)
			return nil
		}
		sourceChainID, err := ids.ToID(sourceChainIDIntf.([]byte))
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid chain id",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Uint32("requestID", requestID),
				zap.Error(err),
			)
			return nil
		}
		appBytesIntf, err := msg.Get(message.AppBytes)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Stringer("field", message.AppBytes),
				zap.Error(err),
			)
			return nil
		}
		appBytes := appBytesIntf.([]byte)
		return engine.CrossChainAppRequest(ctx, sourceChainID, requestID, msg.ExpirationTime(), appBytes)

	case message.CrossChainAppResponse:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)
		sourceChainIDIntf, err := msg.Get(message.SourceChainID)
		if err != nil {
			return err
		}
		sourceChainID, err := ids.ToID(sourceChainIDIntf.([]byte))
		if err != nil {
			return err
		}
		appBytesIntf, err := msg.Get(message.AppBytes)
		if err != nil {
			h.ctx.Log.Debug("dropping message with invalid field",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("messageOp", op),
				zap.Stringer("field", message.AppBytes),
				zap.Error(err),
			)
			return engine.CrossChainAppRequestFailed(ctx, sourceChainID, requestID)
		}
		appBytes := appBytesIntf.([]byte)
		return engine.CrossChainAppResponse(ctx, sourceChainID, requestID, appBytes)

	case message.CrossChainAppRequestFailed:
		requestIDIntf, err := msg.Get(message.RequestID)
		if err != nil {
			return err
		}
		requestID := requestIDIntf.(uint32)
		sourceChainIDIntf, err := msg.Get(message.SourceChainID)
		if err != nil {
			return err
		}
		sourceChainID, err := ids.ToID(sourceChainIDIntf.([]byte))
		if err != nil {
			return err
		}
		return engine.CrossChainAppRequestFailed(ctx, sourceChainID, requestID)

	default:
		return fmt.Errorf(
			"attempt to submit unhandled async msg %s from %s",
			op, nodeID,
		)
	}
}

// Any returned error is treated as fatal
func (h *handler) handleChanMsg(msg message.InboundMessage) error {
	h.ctx.Log.Debug("forwarding chan message to consensus",
		zap.Stringer("messageString", msg),
	)

	var (
		op        = msg.Op()
		startTime = h.clock.Time()
	)
	h.ctx.Lock.Lock()
	defer func() {
		h.ctx.Lock.Unlock()

		var (
			endTime   = h.clock.Time()
			histogram = h.metrics.messages[op]
		)
		histogram.Observe(float64(endTime.Sub(startTime)))
		msg.OnFinishedHandling()
		h.ctx.Log.Debug("finished handling chan message",
			zap.Stringer("messageOp", op),
		)
	}()

	engine, err := h.getEngine()
	if err != nil {
		return err
	}

	switch op := msg.Op(); op {
	case message.Notify:
		vmMsgIntf, err := msg.Get(message.VMMessage)
		if err != nil {
			return err
		}
		vmMsg := vmMsgIntf.(uint32)

		return engine.Notify(common.Message(vmMsg))

	case message.GossipRequest:
		return engine.Gossip()

	case message.Timeout:
		return engine.Timeout()

	default:
		return fmt.Errorf(
			"attempt to submit unhandled chan msg %s",
			op,
		)
	}
}

func (h *handler) getEngine() (common.Engine, error) {
	state := h.ctx.GetState()
	switch state {
	case snow.StateSyncing:
		return h.stateSyncer, nil
	case snow.Bootstrapping:
		return h.bootstrapper, nil
	case snow.NormalOp:
		return h.engine, nil
	default:
		return nil, fmt.Errorf("unknown handler for state %s", state)
	}
}

func (h *handler) popUnexpiredMsg(queue MessageQueue, expired prometheus.Counter) (context.Context, message.InboundMessage, bool) {
	for {
		// Get the next message we should process. If the handler is shutting
		// down, we may fail to pop a message.
		ctx, msg, ok := queue.Pop()
		if !ok {
			return context.Background(), nil, false
		}

		// If this message's deadline has passed, don't process it.
		if expirationTime := msg.ExpirationTime(); !expirationTime.IsZero() && h.clock.Time().After(expirationTime) {
			h.ctx.Log.Verbo("dropping message",
				zap.String("reason", "timeout"),
				zap.Stringer("nodeID", msg.NodeID()),
				zap.Stringer("messageString", msg),
			)
			span := trace.SpanFromContext(ctx)
			span.AddEvent("dropping message", trace.WithAttributes(
				attribute.String("reason", "timeout"),
			))
			expired.Inc()
			msg.OnFinishedHandling()
			continue
		}

		return ctx, msg, true
	}
}

func (h *handler) closeDispatcher() {
	h.ctx.Lock.Lock()
	defer h.ctx.Lock.Unlock()

	h.numDispatchersClosed++
	if h.numDispatchersClosed < numDispatchersToClose {
		return
	}

	h.shutdown()
}

func (h *handler) shutdown() {
	defer func() {
		if h.onStopped != nil {
			go h.onStopped()
		}
		close(h.closed)
	}()

	currentEngine, err := h.getEngine()
	if err != nil {
		h.ctx.Log.Error("failed fetching current engine during shutdown",
			zap.Error(err),
		)
		return
	}

	if err := currentEngine.Shutdown(); err != nil {
		h.ctx.Log.Error("failed while shutting down the chain",
			zap.Error(err),
		)
	}
}

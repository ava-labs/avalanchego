// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/networking/worker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/uptime"
	"github.com/ava-labs/avalanchego/version"
)

const (
	cpuHalflife           = 15 * time.Second
	threadPoolSize        = 2
	numDispatchersToClose = 3
)

var _ Handler = &handler{}

type Handler interface {
	common.Timer

	Context() *snow.ConsensusContext
	IsValidator(nodeID ids.ShortID) bool

	SetBootstrapper(engine common.BootstrapableEngine)
	Bootstrapper() common.BootstrapableEngine

	SetConsensus(engine common.Engine)
	Consensus() common.Engine

	SetOnStopped(onStopped func())

	Start(recoverPanic bool)
	Push(msg message.InboundMessage)
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
	mc  message.Creator
	// The validator set that validates this chain
	validators validators.Set
	// Receives messages from the VM
	msgFromVMChan   <-chan common.Message
	preemptTimeouts chan struct{}
	gossipFrequency time.Duration

	bootstrapper common.BootstrapableEngine
	engine       common.Engine
	// onStopped is called in a goroutine when this handler finishes shutting
	// down. If it is nil then it is skipped.
	onStopped func()

	// Tracks CPU time spent processing messages from each node
	cpuTracker tracker.TimeTracker
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
	mc message.Creator,
	ctx *snow.ConsensusContext,
	validators validators.Set,
	msgFromVMChan <-chan common.Message,
	preemptTimeouts chan struct{},
	gossipFrequency time.Duration,
) (Handler, error) {
	h := &handler{
		ctx:             ctx,
		mc:              mc,
		validators:      validators,
		msgFromVMChan:   msgFromVMChan,
		preemptTimeouts: preemptTimeouts,
		gossipFrequency: gossipFrequency,

		cpuTracker:       tracker.NewCPUTracker(uptime.ContinuousFactory{}, cpuHalflife),
		asyncMessagePool: worker.NewPool(threadPoolSize),
		timeouts:         make(chan struct{}, 1),

		closingChan: make(chan struct{}),
		closed:      make(chan struct{}),
	}

	var err error
	h.metrics, err = newMetrics("handler", h.ctx.Registerer)
	if err != nil {
		return nil, fmt.Errorf("initializing handler metrics errored with: %w", err)
	}
	h.syncMessageQueue, err = NewMessageQueue(h.ctx.Log, h.validators, h.cpuTracker, "handler", h.ctx.Registerer)
	if err != nil {
		return nil, fmt.Errorf("initializing sync message queue errored with: %w", err)
	}
	h.asyncMessageQueue, err = NewMessageQueue(h.ctx.Log, h.validators, h.cpuTracker, "handler_async", h.ctx.Registerer)
	if err != nil {
		return nil, fmt.Errorf("initializing async message queue errored with: %w", err)
	}
	return h, nil
}

func (h *handler) Context() *snow.ConsensusContext { return h.ctx }

func (h *handler) IsValidator(nodeID ids.ShortID) bool {
	return !h.ctx.IsValidatorOnly() ||
		nodeID == h.ctx.NodeID ||
		h.validators.Contains(nodeID)
}

func (h *handler) SetBootstrapper(engine common.BootstrapableEngine) { h.bootstrapper = engine }
func (h *handler) Bootstrapper() common.BootstrapableEngine          { return h.bootstrapper }

func (h *handler) SetConsensus(engine common.Engine) { h.engine = engine }
func (h *handler) Consensus() common.Engine          { return h.engine }

func (h *handler) SetOnStopped(onStopped func()) { h.onStopped = onStopped }

func (h *handler) Start(recoverPanic bool) {
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

// Push the message onto the handler's queue
func (h *handler) Push(msg message.InboundMessage) {
	switch msg.Op() {
	case message.AppRequest, message.AppGossip, message.AppRequestFailed, message.AppResponse:
		h.asyncMessageQueue.Push(msg)
	default:
		h.syncMessageQueue.Push(msg)
	}
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
	h.ctx.Log.Fatal("shutting down chain due to unexpected error: %s", err)
	h.Stop()
}

func (h *handler) Stopped() chan struct{} { return h.closed }

func (h *handler) dispatchSync() {
	defer h.closeDispatcher()

	// Handle sync messages from the router
	for {
		// Get the next message we should process. If the handler is shutting
		// down, we may fail to pop a message.
		msg, ok := h.popUnexpiredMsg(h.syncMessageQueue)
		if !ok {
			return
		}

		// If there is an error handling the message, shut down the chain
		if err := h.handleSyncMsg(msg); err != nil {
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
		msg, ok := h.popUnexpiredMsg(h.asyncMessageQueue)
		if !ok {
			return
		}

		h.handleAsyncMsg(msg)
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

func (h *handler) handleSyncMsg(msg message.InboundMessage) error {
	h.ctx.Log.Debug("Forwarding sync message to consensus: %s", msg)

	var (
		nodeID    = msg.NodeID()
		op        = msg.Op()
		startTime = h.clock.Time()
	)
	h.cpuTracker.StartCPU(nodeID, startTime)
	h.ctx.Lock.Lock()
	defer func() {
		h.ctx.Lock.Unlock()

		var (
			endTime   = h.clock.Time()
			histogram = h.metrics.messages[op]
		)
		h.cpuTracker.StopCPU(nodeID, endTime)
		histogram.Observe(float64(endTime.Sub(startTime)))
		msg.OnFinishedHandling()
		h.ctx.Log.Debug("Finished handling sync message: %s", op)
	}()

	engine, err := h.getEngine()
	if err != nil {
		return err
	}

	switch op {
	case message.GetAcceptedFrontier:
		reqID := msg.Get(message.RequestID).(uint32)
		return engine.GetAcceptedFrontier(nodeID, reqID)

	case message.AcceptedFrontier:
		reqID := msg.Get(message.RequestID).(uint32)
		containerIDs, err := getContainerIDs(msg)
		if err != nil {
			h.ctx.Log.Debug(
				"Malformed message %s from (%s%s, %d): %s",
				op,
				constants.NodeIDPrefix,
				nodeID,
				reqID,
				err,
			)
			return engine.GetAcceptedFrontierFailed(nodeID, reqID)
		}
		return engine.AcceptedFrontier(nodeID, reqID, containerIDs)

	case message.GetAcceptedFrontierFailed:
		reqID := msg.Get(message.RequestID).(uint32)
		return engine.GetAcceptedFrontierFailed(nodeID, reqID)

	case message.GetAccepted:
		reqID := msg.Get(message.RequestID).(uint32)
		containerIDs, err := getContainerIDs(msg)
		if err != nil {
			h.ctx.Log.Debug(
				"Malformed message %s from (%s%s, %d): %s",
				op,
				constants.NodeIDPrefix,
				nodeID,
				reqID,
				err,
			)
			return nil
		}
		return engine.GetAccepted(nodeID, reqID, containerIDs)

	case message.Accepted:
		reqID := msg.Get(message.RequestID).(uint32)
		containerIDs, err := getContainerIDs(msg)
		if err != nil {
			h.ctx.Log.Debug(
				"Malformed message %s from (%s%s, %d): %s",
				op,
				constants.NodeIDPrefix,
				nodeID,
				reqID,
				err,
			)
			return engine.GetAcceptedFailed(nodeID, reqID)
		}
		return engine.Accepted(nodeID, reqID, containerIDs)

	case message.GetAcceptedFailed:
		reqID := msg.Get(message.RequestID).(uint32)
		return engine.GetAcceptedFailed(nodeID, reqID)

	case message.GetAncestors:
		reqID := msg.Get(message.RequestID).(uint32)
		containerID, err := ids.ToID(msg.Get(message.ContainerID).([]byte))
		h.ctx.Log.AssertNoError(err)
		return engine.GetAncestors(nodeID, reqID, containerID)

	case message.GetAncestorsFailed:
		reqID := msg.Get(message.RequestID).(uint32)
		return engine.GetAncestorsFailed(nodeID, reqID)

	case message.Ancestors:
		reqID := msg.Get(message.RequestID).(uint32)
		containers := msg.Get(message.MultiContainerBytes).([][]byte)
		return engine.Ancestors(nodeID, reqID, containers)

	case message.Get:
		reqID := msg.Get(message.RequestID).(uint32)
		containerID, err := ids.ToID(msg.Get(message.ContainerID).([]byte))
		h.ctx.Log.AssertNoError(err)
		return engine.Get(nodeID, reqID, containerID)

	case message.GetFailed:
		reqID := msg.Get(message.RequestID).(uint32)
		return engine.GetFailed(nodeID, reqID)

	case message.Put:
		reqID := msg.Get(message.RequestID).(uint32)
		container := msg.Get(message.ContainerBytes).([]byte)
		return engine.Put(nodeID, reqID, container)

	case message.PushQuery:
		reqID := msg.Get(message.RequestID).(uint32)
		container := msg.Get(message.ContainerBytes).([]byte)
		return engine.PushQuery(nodeID, reqID, container)

	case message.PullQuery:
		reqID := msg.Get(message.RequestID).(uint32)
		containerID, err := ids.ToID(msg.Get(message.ContainerID).([]byte))
		h.ctx.Log.AssertNoError(err)
		return engine.PullQuery(nodeID, reqID, containerID)

	case message.Chits:
		reqID := msg.Get(message.RequestID).(uint32)
		votes, err := getContainerIDs(msg)
		if err != nil {
			h.ctx.Log.Debug(
				"Malformed message %s from (%s%s, %d): %s",
				op,
				constants.NodeIDPrefix,
				nodeID,
				reqID,
				err,
			)
			return engine.QueryFailed(nodeID, reqID)
		}
		return engine.Chits(nodeID, reqID, votes)

	case message.QueryFailed:
		reqID := msg.Get(message.RequestID).(uint32)
		return engine.QueryFailed(nodeID, reqID)

	case message.Connected:
		peerVersion := msg.Get(message.VersionStruct).(version.Application)
		return engine.Connected(nodeID, peerVersion)

	case message.Disconnected:
		return engine.Disconnected(nodeID)

	default:
		return fmt.Errorf(
			"attempt to submit unhandled sync msg %s from %s%s",
			op,
			constants.NodeIDPrefix,
			nodeID,
		)
	}
}

func (h *handler) handleAsyncMsg(msg message.InboundMessage) {
	h.asyncMessagePool.Send(func() {
		if err := h.executeAsyncMsg(msg); err != nil {
			h.StopWithError(fmt.Errorf(
				"%w while processing async message: %s",
				err,
				msg,
			))
		}
	})
}

func (h *handler) executeAsyncMsg(msg message.InboundMessage) error {
	h.ctx.Log.Debug("Forwarding async message to consensus: %s", msg)

	var (
		nodeID    = msg.NodeID()
		op        = msg.Op()
		startTime = h.clock.Time()
	)
	h.cpuTracker.StartCPU(nodeID, startTime)
	defer func() {
		var (
			endTime   = h.clock.Time()
			histogram = h.metrics.messages[op]
		)
		h.cpuTracker.StopCPU(nodeID, endTime)
		histogram.Observe(float64(endTime.Sub(startTime)))
		msg.OnFinishedHandling()
		h.ctx.Log.Debug("Finished handling async message: %s", op)
	}()

	engine, err := h.getEngine()
	if err != nil {
		return err
	}

	switch op {
	case message.AppRequest:
		reqID := msg.Get(message.RequestID).(uint32)
		appBytes := msg.Get(message.AppBytes).([]byte)
		return engine.AppRequest(nodeID, reqID, msg.ExpirationTime(), appBytes)

	case message.AppResponse:
		reqID := msg.Get(message.RequestID).(uint32)
		appBytes := msg.Get(message.AppBytes).([]byte)
		return engine.AppResponse(nodeID, reqID, appBytes)

	case message.AppRequestFailed:
		reqID := msg.Get(message.RequestID).(uint32)
		return engine.AppRequestFailed(nodeID, reqID)

	case message.AppGossip:
		appBytes := msg.Get(message.AppBytes).([]byte)
		return engine.AppGossip(nodeID, appBytes)

	default:
		return fmt.Errorf(
			"attempt to submit unhandled async msg %s from %s%s",
			op,
			constants.NodeIDPrefix,
			nodeID,
		)
	}
}

func (h *handler) handleChanMsg(msg message.InboundMessage) error {
	h.ctx.Log.Debug("Forwarding chan message to consensus: %s", msg)

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
		h.ctx.Log.Debug("Finished handling chan message: %s", op)
	}()

	engine, err := h.getEngine()
	if err != nil {
		return err
	}

	switch op := msg.Op(); op {
	case message.Notify:
		vmMsg := msg.Get(message.VMMessage).(uint32)
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
	case snow.Bootstrapping:
		return h.bootstrapper, nil
	case snow.NormalOp:
		return h.engine, nil
	default:
		return nil, fmt.Errorf("unknown handler for state %s", state)
	}
}

func (h *handler) popUnexpiredMsg(queue MessageQueue) (message.InboundMessage, bool) {
	for {
		// Get the next message we should process. If the handler is shutting
		// down, we may fail to pop a message.
		msg, ok := queue.Pop()
		if !ok {
			return nil, false
		}

		// If this message's deadline has passed, don't process it.
		if expirationTime := msg.ExpirationTime(); !expirationTime.IsZero() && h.clock.Time().After(expirationTime) {
			h.ctx.Log.Verbo(
				"Dropping message from %s%s due to timeout: %s",
				constants.NodeIDPrefix,
				msg.NodeID(),
				msg,
			)
			h.metrics.expired.Inc()
			msg.OnFinishedHandling()
			continue
		}

		return msg, true
	}
}

func (h *handler) closeDispatcher() {
	h.ctx.Lock.Lock()
	defer h.ctx.Lock.Unlock()

	h.numDispatchersClosed++
	if h.numDispatchersClosed < numDispatchersToClose {
		return
	}

	if err := h.engine.Shutdown(); err != nil {
		h.ctx.Log.Error("Error while shutting down the chain: %s", err)
	}
	if h.onStopped != nil {
		go h.onStopped()
	}
	close(h.closed)
}

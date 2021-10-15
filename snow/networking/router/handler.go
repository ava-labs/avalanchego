// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/uptime"
)

// Handler passes incoming messages from the network to the consensus engine.
// (Actually, it receives the incoming messages from a ChainRouter, but same difference.)
type Handler struct {
	ctx *snow.Context
	// Useful for faking time in tests
	clock   timer.Clock
	mc      message.Creator
	metrics handlerMetrics
	// The validator set that validates this chain
	validators validators.Set
	// The consensus engine
	engine common.Engine
	// Closed when this handler and [engine] are done shutting down
	closed chan struct{}
	// Receives messages from the VM
	msgFromVMChan <-chan common.Message
	// Tracks CPU time spent processing messages from each node
	cpuTracker tracker.TimeTracker
	// Called in a goroutine when this handler/engine shuts down.
	// May be nil.
	onCloseF            func()
	unprocessedMsgsCond *sync.Cond
	// Holds messages that [engine] hasn't processed yet.
	// [unprocessedMsgsCond.L] must be held while accessing [unprocessedMsgs].
	unprocessedMsgs unprocessedMsgs
	closing         utils.AtomicBool
}

// Initialize this consensus handler
// [engine] must be initialized before initializing this handler
func (h *Handler) Initialize(
	mc message.Creator,
	engine common.Engine,
	validators validators.Set,
	msgFromVMChan <-chan common.Message,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) error {
	h.ctx = engine.Context()
	if err := h.metrics.Initialize(metricsNamespace, metricsRegisterer); err != nil {
		return fmt.Errorf("initializing handler metrics errored with: %s", err)
	}
	h.mc = mc
	h.closed = make(chan struct{})
	h.msgFromVMChan = msgFromVMChan
	h.engine = engine
	h.validators = validators
	var lock sync.Mutex
	h.unprocessedMsgsCond = sync.NewCond(&lock)
	h.cpuTracker = tracker.NewCPUTracker(uptime.IntervalFactory{}, defaultCPUInterval)
	var err error
	h.unprocessedMsgs, err = newUnprocessedMsgs(h.ctx.Log, h.validators, h.cpuTracker, metricsNamespace, metricsRegisterer)
	return err
}

// Context of this Handler
func (h *Handler) Context() *snow.Context { return h.engine.Context() }

// Engine returns the engine this handler dispatches to
func (h *Handler) Engine() common.Engine { return h.engine }

// SetEngine sets the engine for this handler to dispatch to
func (h *Handler) SetEngine(engine common.Engine) { h.engine = engine }

// Dispatch waits for incoming messages from the router
// and, when they arrive, sends them to the consensus engine
func (h *Handler) Dispatch() {
	defer h.shutdown()

	// Handle messages from the VM
	go h.dispatchInternal()

	// Handle messages from the router
	for {
		// Wait until there is an unprocessed message
		h.unprocessedMsgsCond.L.Lock()
		for {
			if closing := h.closing.GetValue(); closing {
				h.unprocessedMsgsCond.L.Unlock()
				return
			}
			if h.unprocessedMsgs.Len() == 0 {
				// Signalled in [h.push] and [h.StartShutdown]
				h.unprocessedMsgsCond.Wait()
				continue
			}
			break
		}

		// Get the next message we should process
		msg := h.unprocessedMsgs.Pop()
		h.unprocessedMsgsCond.L.Unlock()

		// If this message's deadline has passed, don't process it.
		if !msg.deadline.IsZero() && h.clock.Time().After(msg.deadline) {
			h.ctx.Log.Verbo("Dropping message from %s%s due to timeout. msg: %s", constants.NodeIDPrefix, msg.nodeID, msg)
			h.metrics.expired.Inc()
			msg.doneHandling()
			continue
		}

		// Process the message.
		// If there was an error, shut down this chain
		if err := h.handleMsg(msg); err != nil {
			h.ctx.Log.Fatal("chain shutting down due to error %q while processing message: %s", err, msg)
			h.StartShutdown()
			return
		}
	}
}

// Dispatch a message to the consensus engine.
func (h *Handler) handleMsg(wrp messageWrap) error {
	startTime := h.clock.Time()

	isPeriodic := wrp.IsPeriodic()
	if isPeriodic {
		h.ctx.Log.Verbo("Forwarding message to consensus: %s", wrp)
	} else {
		h.ctx.Log.Debug("Forwarding message to consensus: %s", wrp)
	}

	h.ctx.Lock.Lock()
	defer h.ctx.Lock.Unlock()

	var err error
	msgType := wrp.inMsg.Op()
	switch msgType {
	case message.Notify:
		vmMsg := wrp.inMsg.Get(message.VMMessage).(uint32)
		err = h.engine.Notify(common.Message(vmMsg))
		h.metrics.notify.Observe(float64(h.clock.Time().Sub(startTime)))
	case message.GossipRequest:
		err = h.engine.Gossip()
		h.metrics.gossip.Observe(float64(h.clock.Time().Sub(startTime)))
	case message.Timeout:
		err = h.engine.Timeout()
		h.metrics.timeout.Observe(float64(h.clock.Time().Sub(startTime)))
	default:
		err = h.handleConsensusMsg(wrp)
		endTime := h.clock.Time()
		handleDuration := endTime.Sub(startTime)
		histogram := h.metrics.getMSGHistogram(wrp.inMsg.Op())
		histogram.Observe(float64(handleDuration))
		h.cpuTracker.UtilizeTime(wrp.nodeID, startTime, endTime)
	}

	wrp.doneHandling()

	if isPeriodic {
		h.ctx.Log.Verbo("Finished handling message: %s", msgType)
	} else {
		h.ctx.Log.Debug("Finished handling message: %s", msgType)
	}
	return err
}

// Assumes [h.ctx.Lock] is locked
// Relevant fields in msgs must be validated before being dispatched to the engine.
// An invalid msg is logged and dropped silently since err would cause a chain shutdown.
func (h *Handler) handleConsensusMsg(wrp messageWrap) error {
	switch wrp.inMsg.Op() {
	case message.GetAcceptedFrontier:
		return h.engine.GetAcceptedFrontier(wrp.nodeID, wrp.requestID)

	case message.AcceptedFrontier:
		containerIDs, err := message.DecodeContainerIDs(wrp.inMsg)
		if err != nil {
			h.ctx.Log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: %s",
				wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID, err)
			return nil
		}
		return h.engine.AcceptedFrontier(wrp.nodeID, wrp.requestID, containerIDs)

	case message.GetAcceptedFrontierFailed:
		return h.engine.GetAcceptedFrontierFailed(wrp.nodeID, wrp.requestID)

	case message.GetAccepted:
		containerIDs, err := message.DecodeContainerIDs(wrp.inMsg)
		if err != nil {
			h.ctx.Log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: %s",
				wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID, err)
			return nil
		}
		return h.engine.GetAccepted(wrp.nodeID, wrp.requestID, containerIDs)

	case message.Accepted:
		containerIDs, err := message.DecodeContainerIDs(wrp.inMsg)
		if err != nil {
			h.ctx.Log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: %s",
				wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID, err)
			return nil
		}
		return h.engine.Accepted(wrp.nodeID, wrp.requestID, containerIDs)

	case message.GetAcceptedFailed:
		return h.engine.GetAcceptedFailed(wrp.nodeID, wrp.requestID)

	case message.GetAncestors:
		containerID, err := ids.ToID(wrp.inMsg.Get(message.ContainerID).([]byte))
		if err != nil {
			h.ctx.Log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: %s",
				wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID, err)
			return nil
		}
		return h.engine.GetAncestors(wrp.nodeID, wrp.requestID, containerID)
	case message.GetAncestorsFailed:
		return h.engine.GetAncestorsFailed(wrp.nodeID, wrp.requestID)

	case message.MultiPut:
		containers, ok := wrp.inMsg.Get(message.MultiContainerBytes).([][]byte)
		if !ok {
			h.ctx.Log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: could not parse MultiContainerBytes",
				wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID)
			return nil
		}
		return h.engine.MultiPut(wrp.nodeID, wrp.requestID, containers)

	case message.Get:
		containerID, err := ids.ToID(wrp.inMsg.Get(message.ContainerID).([]byte))
		h.ctx.Log.AssertNoError(err)
		return h.engine.Get(wrp.nodeID, wrp.requestID, containerID)

	case message.GetFailed:
		return h.engine.GetFailed(wrp.nodeID, wrp.requestID)

	case message.Put:
		containerID, err := ids.ToID(wrp.inMsg.Get(message.ContainerID).([]byte))
		h.ctx.Log.AssertNoError(err)
		container, ok := wrp.inMsg.Get(message.ContainerBytes).([]byte)
		if !ok {
			h.ctx.Log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: could not parse ContainerBytes",
				wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID)
			return nil
		}
		return h.engine.Put(wrp.nodeID, wrp.requestID, containerID, container)

	case message.PushQuery:
		containerID, err := ids.ToID(wrp.inMsg.Get(message.ContainerID).([]byte))
		h.ctx.Log.AssertNoError(err)
		container, ok := wrp.inMsg.Get(message.ContainerBytes).([]byte)
		if !ok {
			h.ctx.Log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: could not parse ContainerBytes",
				wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID)
			return nil
		}
		return h.engine.PushQuery(wrp.nodeID, wrp.requestID, containerID, container)

	case message.PullQuery:
		containerID, err := ids.ToID(wrp.inMsg.Get(message.ContainerID).([]byte))
		h.ctx.Log.AssertNoError(err)
		return h.engine.PullQuery(wrp.nodeID, wrp.requestID, containerID)

	case message.QueryFailed:
		return h.engine.QueryFailed(wrp.nodeID, wrp.requestID)

	case message.Chits:
		votes, err := message.DecodeContainerIDs(wrp.inMsg)
		if err != nil {
			h.ctx.Log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: %s",
				wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID, err)
			return nil
		}
		return h.engine.Chits(wrp.nodeID, wrp.requestID, votes)

	case message.Connected:
		return h.engine.Connected(wrp.nodeID)

	case message.Disconnected:
		return h.engine.Disconnected(wrp.nodeID)

	case message.AppRequest:
		appRequestBytes, ok := wrp.inMsg.Get(message.AppRequestBytes).([]byte)
		if !ok {
			h.ctx.Log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: could not parse AppRequestBytes",
				wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID)
			return nil
		}
		return h.engine.AppRequest(wrp.nodeID, wrp.requestID, appRequestBytes)

	case message.AppResponse:
		appResponseBytes, ok := wrp.inMsg.Get(message.AppResponseBytes).([]byte)
		if !ok {
			h.ctx.Log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: could not parse AppResponseBytes",
				wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID)
			return nil
		}
		return h.engine.AppResponse(wrp.nodeID, wrp.requestID, appResponseBytes)

	case message.AppGossip:
		appGossipBytes, ok := wrp.inMsg.Get(message.AppGossipBytes).([]byte)
		if !ok {
			h.ctx.Log.Debug("Malformed message %s from (%s, %s, %d) dropped. Error: could not parse AppGossipBytes",
				wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID)
			return nil
		}
		return h.engine.AppGossip(wrp.nodeID, appGossipBytes)

	default:
		h.ctx.Log.Warn("Attempt to submit to engine unhandled consensus msg %s from from (%s, %s, %d). Dropping it",
			wrp.inMsg.Op(), wrp.nodeID, h.engine.Context().ChainID, wrp.requestID)
		return nil
	}
}

func (h *Handler) PushMsgWithDeadline(
	inMsg message.InboundMessage,
	nodeID ids.ShortID,
	requestID uint32) {
	received := h.clock.Time()
	deadline := received.Add(time.Duration(inMsg.Get(message.Deadline).(uint64)))

	h.push(messageWrap{
		inMsg:     inMsg,
		nodeID:    nodeID,
		requestID: requestID,
		received:  received,
		deadline:  deadline,
	})
}

func (h *Handler) PushMsgWithoutDeadline(
	inMsg message.InboundMessage,
	nodeID ids.ShortID,
	requestID uint32) {
	received := h.clock.Time()

	h.push(messageWrap{
		inMsg:     inMsg,
		nodeID:    nodeID,
		requestID: requestID,
		received:  received,
	})
}

// Timeout passes a new timeout notification to the consensus engine
func (h *Handler) Timeout() {
	inMsg := h.mc.InternalTimeout(h.ctx.NodeID)
	h.push(messageWrap{
		inMsg:  inMsg,
		nodeID: h.ctx.NodeID,
	})
}

// Connected passes a new connection notification to the consensus engine
func (h *Handler) Connected(nodeID ids.ShortID) {
	inMsg := h.mc.InternalConnected(h.ctx.NodeID)
	h.push(messageWrap{
		inMsg:  inMsg,
		nodeID: nodeID,
	})
}

// Disconnected passes a new connection notification to the consensus engine
func (h *Handler) Disconnected(nodeID ids.ShortID) {
	inMsg := h.mc.InternalDisconnected(h.ctx.NodeID)
	h.push(messageWrap{
		inMsg:  inMsg,
		nodeID: nodeID,
	})
}

// Gossip passes a gossip request to the consensus engine
func (h *Handler) Gossip() {
	if !h.ctx.IsBootstrapped() {
		// Shouldn't send gossiping messages while the chain is bootstrapping
		return
	}

	inMsg := h.mc.InternalGossipRequest(h.ctx.NodeID)
	h.push(messageWrap{
		inMsg:  inMsg,
		nodeID: h.ctx.NodeID,
	})
}

// StartShutdown starts the shutdown process for this handler/engine.
// [h] must never be invoked again after calling this method.
// This method causes [shutdown] to eventually be called.
// [h.closed] is closed when this handler/engine are done shutting down.
func (h *Handler) StartShutdown() {
	// Must hold [h.unprocessedMsgsCond.L] here to ensure
	// there's no race condition in Dispatch where we check
	// the value of [h.closing].
	h.unprocessedMsgsCond.L.Lock()
	h.closing.SetValue(true)
	h.unprocessedMsgsCond.L.Unlock()

	// If we're waiting in [Dispatch] wake up.
	h.unprocessedMsgsCond.Signal()
	// Don't process any more bootstrap messages.
	// If [h.engine] is processing a bootstrap message, stop.
	// We do this because if we didn't, and the engine was in the
	// middle of executing state transitions during bootstrapping,
	// we wouldn't be able to grab [h.ctx.Lock] until the engine
	// finished executing state transitions, which may take a long time.
	// As a result, the router would time out on shutting down this chain.
	h.engine.Halt()
}

// Calls [h.engine.Shutdown] and [h.onCloseF]; closes [h.closed].
func (h *Handler) shutdown() {
	h.ctx.Lock.Lock()
	defer h.ctx.Lock.Unlock()

	startTime := h.clock.Time()
	if err := h.engine.Shutdown(); err != nil {
		h.ctx.Log.Error("Error while shutting down the chain: %s", err)
	}
	if h.onCloseF != nil {
		go h.onCloseF()
	}
	endTime := h.clock.Time()
	h.metrics.shutdown.Observe(float64(endTime.Sub(startTime)))
	close(h.closed)
}

// Assumes [h.unprocessedMsgsCond.L] is not held
func (h *Handler) push(msg messageWrap) {
	if msg.nodeID == ids.ShortEmpty {
		// This should never happen
		h.ctx.Log.Warn("message does not have node ID of sender. Message: %s", msg)
	}

	h.unprocessedMsgsCond.L.Lock()
	defer h.unprocessedMsgsCond.L.Unlock()

	h.unprocessedMsgs.Push(msg)
	h.unprocessedMsgsCond.Signal()
}

func (h *Handler) dispatchInternal() {
	for {
		select {
		case <-h.closed:
			return
		case msg := <-h.msgFromVMChan:
			if closing := h.closing.GetValue(); closing {
				return
			}
			// handle a message from the VM
			inMsg := h.mc.InternalVMMessage(h.ctx.NodeID, uint32(msg))
			h.push(messageWrap{
				inMsg:  inMsg,
				nodeID: h.ctx.NodeID,
			})
		}
	}
}

func (h *Handler) endInterval() {
	now := h.clock.Time()
	h.cpuTracker.EndInterval(now)
}

// if subnet is validator only and this is not a validator or self, returns false.
func (h *Handler) isValidator(nodeID ids.ShortID) bool {
	return !h.ctx.IsValidatorOnly() || nodeID == h.ctx.NodeID || h.validators.Contains(nodeID)
}

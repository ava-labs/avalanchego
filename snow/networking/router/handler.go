// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
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
// engine must be initialized before initializing the handler
func (h *Handler) Initialize(
	engine common.Engine,
	validators validators.Set,
	msgFromVMChan <-chan common.Message,
	namespace string,
	metrics prometheus.Registerer,
) error {
	h.ctx = engine.Context()
	if err := h.metrics.Initialize(namespace, metrics); err != nil {
		return fmt.Errorf("initializing handler metrics errored with: %s", err)
	}
	h.closed = make(chan struct{})
	h.msgFromVMChan = msgFromVMChan
	h.engine = engine
	h.validators = validators
	var lock sync.Mutex
	h.unprocessedMsgsCond = sync.NewCond(&lock)
	h.cpuTracker = tracker.NewCPUTracker(uptime.IntervalFactory{}, defaultCPUInterval)
	h.unprocessedMsgs = newUnprocessedMsgs(h.ctx.Log, h.validators, h.cpuTracker)
	return nil
}

// Context of this Handler
func (h *Handler) Context() *snow.Context { return h.engine.Context() }

// Engine returns the engine this handler dispatches to
func (h *Handler) Engine() common.Engine { return h.engine }

// SetEngine sets the engine for this handler to dispatch to
func (h *Handler) SetEngine(engine common.Engine) { h.engine = engine }

// Dispatch waits for incoming messages from the network
// and, when they arrive, sends them to the consensus engine
func (h *Handler) Dispatch() {
	defer h.shutdown()

	// Handle messages from the VM
	go h.dispatchInternal()

	// Handle messages from the network
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
			h.metrics.dropped.Inc()
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
func (h *Handler) handleMsg(msg message) error {
	startTime := h.clock.Time()

	isPeriodic := msg.IsPeriodic()
	if isPeriodic {
		h.ctx.Log.Verbo("Forwarding message to consensus: %s", msg)
	} else {
		h.ctx.Log.Debug("Forwarding message to consensus: %s", msg)
	}

	h.ctx.Lock.Lock()
	defer h.ctx.Lock.Unlock()

	var err error
	switch msg.messageType {
	case constants.NotifyMsg:
		err = h.engine.Notify(msg.notification)
		h.metrics.notify.Observe(float64(h.clock.Time().Sub(startTime)))
	case constants.GossipMsg:
		err = h.engine.Gossip()
		h.metrics.gossip.Observe(float64(h.clock.Time().Sub(startTime)))
	case constants.TimeoutMsg:
		err = h.engine.Timeout()
		h.metrics.timeout.Observe(float64(h.clock.Time().Sub(startTime)))
	default:
		err = h.handleConsensusMsg(msg, startTime)
	}

	msg.doneHandling()

	if isPeriodic {
		h.ctx.Log.Verbo("Finished handling message: %s", msg.messageType)
	} else {
		h.ctx.Log.Debug("Finished handling message: %s", msg.messageType)
	}
	return err
}

// Assumes [h.ctx.Lock] is locked
func (h *Handler) handleConsensusMsg(msg message, startTime time.Time) error {
	var err error
	switch msg.messageType {
	case constants.GetAcceptedFrontierMsg:
		err = h.engine.GetAcceptedFrontier(msg.nodeID, msg.requestID)
	case constants.AcceptedFrontierMsg:
		err = h.engine.AcceptedFrontier(msg.nodeID, msg.requestID, msg.containerIDs)
	case constants.GetAcceptedFrontierFailedMsg:
		err = h.engine.GetAcceptedFrontierFailed(msg.nodeID, msg.requestID)
	case constants.GetAcceptedMsg:
		err = h.engine.GetAccepted(msg.nodeID, msg.requestID, msg.containerIDs)
	case constants.AcceptedMsg:
		err = h.engine.Accepted(msg.nodeID, msg.requestID, msg.containerIDs)
	case constants.GetAcceptedFailedMsg:
		err = h.engine.GetAcceptedFailed(msg.nodeID, msg.requestID)
	case constants.GetAncestorsMsg:
		err = h.engine.GetAncestors(msg.nodeID, msg.requestID, msg.containerID)
	case constants.GetAncestorsFailedMsg:
		err = h.engine.GetAncestorsFailed(msg.nodeID, msg.requestID)
	case constants.MultiPutMsg:
		err = h.engine.MultiPut(msg.nodeID, msg.requestID, msg.containers)
	case constants.GetMsg:
		err = h.engine.Get(msg.nodeID, msg.requestID, msg.containerID)
	case constants.GetFailedMsg:
		err = h.engine.GetFailed(msg.nodeID, msg.requestID)
	case constants.PutMsg:
		err = h.engine.Put(msg.nodeID, msg.requestID, msg.containerID, msg.container)
	case constants.PushQueryMsg:
		err = h.engine.PushQuery(msg.nodeID, msg.requestID, msg.containerID, msg.container)
	case constants.PullQueryMsg:
		err = h.engine.PullQuery(msg.nodeID, msg.requestID, msg.containerID)
	case constants.QueryFailedMsg:
		err = h.engine.QueryFailed(msg.nodeID, msg.requestID)
	case constants.ChitsMsg:
		err = h.engine.Chits(msg.nodeID, msg.requestID, msg.containerIDs)
	case constants.ConnectedMsg:
		err = h.engine.Connected(msg.nodeID)
	case constants.DisconnectedMsg:
		err = h.engine.Disconnected(msg.nodeID)
	}
	endTime := h.clock.Time()
	timeConsumed := endTime.Sub(startTime)
	histogram := h.metrics.getMSGHistogram(msg.messageType)
	histogram.Observe(float64(timeConsumed))
	h.cpuTracker.UtilizeTime(msg.nodeID, startTime, endTime)
	return err
}

// GetAcceptedFrontier passes a GetAcceptedFrontier message received from the
// network to the consensus engine.
func (h *Handler) GetAcceptedFrontier(
	nodeID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	onDoneHandling func(),
) {
	h.push(message{
		messageType:    constants.GetAcceptedFrontierMsg,
		nodeID:         nodeID,
		requestID:      requestID,
		deadline:       deadline,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// AcceptedFrontier passes a AcceptedFrontier message received from the network
// to the consensus engine.
func (h *Handler) AcceptedFrontier(
	nodeID ids.ShortID,
	requestID uint32,
	containerIDs []ids.ID,
	onDoneHandling func(),
) {
	h.push(message{
		messageType:    constants.AcceptedFrontierMsg,
		nodeID:         nodeID,
		requestID:      requestID,
		containerIDs:   containerIDs,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// GetAcceptedFrontierFailed passes a GetAcceptedFrontierFailed message received
// from the network to the consensus engine.
func (h *Handler) GetAcceptedFrontierFailed(nodeID ids.ShortID, requestID uint32) {
	h.push(message{
		messageType: constants.GetAcceptedFrontierFailedMsg,
		nodeID:      nodeID,
		requestID:   requestID,
	})
}

// GetAccepted passes a GetAccepted message received from the
// network to the consensus engine.
func (h *Handler) GetAccepted(
	nodeID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	containerIDs []ids.ID,
	onDoneHandling func(),
) {
	h.push(message{
		messageType:    constants.GetAcceptedMsg,
		nodeID:         nodeID,
		requestID:      requestID,
		deadline:       deadline,
		containerIDs:   containerIDs,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// Accepted passes a Accepted message received from the network to the consensus
// engine.
func (h *Handler) Accepted(
	nodeID ids.ShortID,
	requestID uint32,
	containerIDs []ids.ID,
	onDoneHandling func(),
) {
	h.push(message{
		messageType:    constants.AcceptedMsg,
		nodeID:         nodeID,
		requestID:      requestID,
		containerIDs:   containerIDs,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// GetAcceptedFailed passes a GetAcceptedFailed message received from the
// network to the consensus engine.
func (h *Handler) GetAcceptedFailed(nodeID ids.ShortID, requestID uint32) {
	h.push(message{
		messageType: constants.GetAcceptedFailedMsg,
		nodeID:      nodeID,
		requestID:   requestID,
	})
}

// GetAncestors passes a GetAncestors message received from the network to the consensus engine.
func (h *Handler) GetAncestors(
	nodeID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	onDoneHandling func(),
) {
	h.push(message{
		messageType:    constants.GetAncestorsMsg,
		nodeID:         nodeID,
		requestID:      requestID,
		deadline:       deadline,
		containerID:    containerID,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// MultiPut passes a MultiPut message received from the network to the consensus engine.
func (h *Handler) MultiPut(
	nodeID ids.ShortID,
	requestID uint32,
	containers [][]byte,
	onDoneHandling func(),
) {
	h.push(message{
		messageType:    constants.MultiPutMsg,
		nodeID:         nodeID,
		requestID:      requestID,
		containers:     containers,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// GetAncestorsFailed passes a GetAncestorsFailed message to the consensus engine.
func (h *Handler) GetAncestorsFailed(nodeID ids.ShortID, requestID uint32) {
	h.push(message{
		messageType: constants.GetAncestorsFailedMsg,
		nodeID:      nodeID,
		requestID:   requestID,
	})
}

// Timeout passes a new timeout notification to the consensus engine
func (h *Handler) Timeout() {
	h.push(message{
		messageType: constants.TimeoutMsg,
		nodeID:      h.ctx.NodeID,
	})
}

// Get passes a Get message received from the network to the consensus engine.
func (h *Handler) Get(
	nodeID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	onDoneHandling func(),
) {
	h.push(message{
		messageType:    constants.GetMsg,
		nodeID:         nodeID,
		requestID:      requestID,
		deadline:       deadline,
		containerID:    containerID,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// Put passes a Put message received from the network to the consensus engine.
func (h *Handler) Put(
	nodeID ids.ShortID,
	requestID uint32,
	containerID ids.ID,
	container []byte,
	onDoneHandling func(),
) {
	h.push(message{
		messageType:    constants.PutMsg,
		nodeID:         nodeID,
		requestID:      requestID,
		containerID:    containerID,
		container:      container,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// GetFailed passes a GetFailed message to the consensus engine.
func (h *Handler) GetFailed(nodeID ids.ShortID, requestID uint32) {
	h.push(message{
		messageType: constants.GetFailedMsg,
		nodeID:      nodeID,
		requestID:   requestID,
	})
}

// PushQuery passes a PushQuery message received from the network to the consensus engine.
func (h *Handler) PushQuery(
	nodeID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	container []byte,
	onDoneHandling func(),
) {
	h.push(message{
		messageType:    constants.PushQueryMsg,
		nodeID:         nodeID,
		requestID:      requestID,
		deadline:       deadline,
		containerID:    containerID,
		container:      container,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// PullQuery passes a PullQuery message received from the network to the consensus engine.
func (h *Handler) PullQuery(
	nodeID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	onDoneHandling func(),
) {
	h.push(message{
		messageType:    constants.PullQueryMsg,
		nodeID:         nodeID,
		requestID:      requestID,
		deadline:       deadline,
		containerID:    containerID,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// Chits passes a Chits message received from the network to the consensus engine.
func (h *Handler) Chits(nodeID ids.ShortID, requestID uint32, votes []ids.ID, onDoneHandling func()) {
	h.push(message{
		messageType:    constants.ChitsMsg,
		nodeID:         nodeID,
		requestID:      requestID,
		containerIDs:   votes,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// QueryFailed passes a QueryFailed message received from the network to the consensus engine.
func (h *Handler) QueryFailed(nodeID ids.ShortID, requestID uint32) {
	h.push(message{
		messageType: constants.QueryFailedMsg,
		nodeID:      nodeID,
		requestID:   requestID,
	})
}

// Connected passes a new connection notification to the consensus engine
func (h *Handler) Connected(nodeID ids.ShortID) {
	h.push(message{
		messageType: constants.ConnectedMsg,
		nodeID:      nodeID,
	})
}

// Disconnected passes a new connection notification to the consensus engine
func (h *Handler) Disconnected(nodeID ids.ShortID) {
	h.push(message{
		messageType: constants.DisconnectedMsg,
		nodeID:      nodeID,
	})
}

// Gossip passes a gossip request to the consensus engine
func (h *Handler) Gossip() {
	if !h.ctx.IsBootstrapped() {
		// Shouldn't send gossiping messages while the chain is bootstrapping
		return
	}
	h.push(message{
		messageType: constants.GossipMsg,
		nodeID:      h.ctx.NodeID,
	})
}

// Notify ...
func (h *Handler) Notify(msg common.Message) {
	h.push(message{
		messageType:  constants.NotifyMsg,
		notification: msg,
		nodeID:       h.ctx.NodeID,
	})
}

// StartShutdown starts the shutdown process for this handler/engine.
// [h] must never be invoked again after calling this method.
// This method causes [shutdown] to eventually be called.
// [h.closed] is closed when this handler/engine are done shutting down.
func (h *Handler) StartShutdown() {
	h.closing.SetValue(true)
	// If we're waiting in [Dispatch] wake up.
	h.unprocessedMsgsCond.Broadcast()
	// Don't process any more bootstrap messages.
	// If [h.engine] is processing a bootstrap message, stop.
	// We do this because if we didn't, and the engine was in the
	// middle of executing state transitions during bootstrapping,
	// we wouldn't be able to grab [h.ctx.Lock] until the engine
	// finished executing state transitions, which may take a long time.
	// As a result, the router would time out on shutting down this chain.
	h.engine.Halt()
}

// Shuts down [h.engine] and calls [h.onCloseF].
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
func (h *Handler) push(msg message) {
	if msg.nodeID == ids.ShortEmpty {
		// This should never happen
		h.ctx.Log.Warn("message does not have node ID of sender. Message: %s", msg)
	}
	if msg.messageType == constants.NullMsg {
		// This should never happen
		h.ctx.Log.Warn("message has message type %s", constants.NullMsg)
	}

	h.unprocessedMsgsCond.L.Lock()
	defer h.unprocessedMsgsCond.L.Unlock()

	h.unprocessedMsgs.Push(msg)
	h.unprocessedMsgsCond.Broadcast()
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
			h.push(message{
				messageType:  constants.NotifyMsg,
				notification: msg,
				nodeID:       h.ctx.NodeID,
			})
		}
	}
}

func (h *Handler) endInterval() {
	now := h.clock.Time()
	h.cpuTracker.EndInterval(now)
}

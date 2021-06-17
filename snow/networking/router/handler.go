// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer"
)

// Handler passes incoming messages from the network to the consensus engine.
// (Actually, it receives the incoming messages from a ChainRouter, but same difference.)
type Handler struct {
	ctx     *snow.Context
	clock   timer.Clock
	metrics handlerMetrics
	// The validator set that validates this chain
	validators validators.Set
	engine     common.Engine

	// Closed when this handler and [engine]
	// are done shutting down
	closed        chan struct{}
	msgFromVMChan <-chan common.Message
	cpuTracker    tracker.TimeTracker
	// Called in a goroutine when this handler/engine shuts down.
	// May be nil.
	onCloseF        func()
	closing         utils.AtomicBool
	unprocessedMsgs unprocessedMsgs
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
	h.unprocessedMsgs = newUnprocessedMsgs(h.validators, h.cpuTracker)
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
	defer h.shutdownDispatch()

	// TODO handle shut down signal
	// TODO handle messages from the VM
	// 	case msg := <-h.msgFromVMChan:
	// 		// handle a message from the VM
	// 		h.handleMsg(message{
	// 			messageType:  constants.NotifyMsg,
	// 			notification: msg,
	// 		})
	// 	}

	// Signalled when there are unprocessed messages
	cond := h.unprocessedMsgs.Cond()
	for {
		cond.L.Lock()
		for h.unprocessedMsgs.Len() == 0 && !h.closing.GetValue() {
			cond.Wait()
		}
		if h.closing.GetValue() {
			cond.L.Unlock()
			return
		}
		// Get the next message we should process
		msg := h.unprocessedMsgs.Pop()
		cond.L.Unlock()

		// If this message's deadline has passed, don't process it.
		if !msg.deadline.IsZero() && h.clock.Time().After(msg.deadline) {
			h.ctx.Log.Verbo("Dropping message due to timeout: %s", msg)
			h.metrics.dropped.Inc()
			h.metrics.expired.Inc()
			msg.doneHandling()
			continue
		}

		// Process the message
		err := h.handleMsg(msg)
		// If there was an error, shut down this chain
		if err != nil {
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

func (h *Handler) handleConsensusMsg(msg message, startTime time.Time) error {
	var err error
	switch msg.messageType {
	case constants.GetAcceptedFrontierMsg:
		err = h.engine.GetAcceptedFrontier(msg.validatorID, msg.requestID)
	case constants.AcceptedFrontierMsg:
		err = h.engine.AcceptedFrontier(msg.validatorID, msg.requestID, msg.containerIDs)
	case constants.GetAcceptedFrontierFailedMsg:
		err = h.engine.GetAcceptedFrontierFailed(msg.validatorID, msg.requestID)
	case constants.GetAcceptedMsg:
		err = h.engine.GetAccepted(msg.validatorID, msg.requestID, msg.containerIDs)
	case constants.AcceptedMsg:
		err = h.engine.Accepted(msg.validatorID, msg.requestID, msg.containerIDs)
	case constants.GetAcceptedFailedMsg:
		err = h.engine.GetAcceptedFailed(msg.validatorID, msg.requestID)
	case constants.GetAncestorsMsg:
		err = h.engine.GetAncestors(msg.validatorID, msg.requestID, msg.containerID)
	case constants.GetAncestorsFailedMsg:
		err = h.engine.GetAncestorsFailed(msg.validatorID, msg.requestID)
	case constants.MultiPutMsg:
		err = h.engine.MultiPut(msg.validatorID, msg.requestID, msg.containers)
	case constants.GetMsg:
		err = h.engine.Get(msg.validatorID, msg.requestID, msg.containerID)
	case constants.GetFailedMsg:
		err = h.engine.GetFailed(msg.validatorID, msg.requestID)
	case constants.PutMsg:
		err = h.engine.Put(msg.validatorID, msg.requestID, msg.containerID, msg.container)
	case constants.PushQueryMsg:
		err = h.engine.PushQuery(msg.validatorID, msg.requestID, msg.containerID, msg.container)
	case constants.PullQueryMsg:
		err = h.engine.PullQuery(msg.validatorID, msg.requestID, msg.containerID)
	case constants.QueryFailedMsg:
		err = h.engine.QueryFailed(msg.validatorID, msg.requestID)
	case constants.ChitsMsg:
		err = h.engine.Chits(msg.validatorID, msg.requestID, msg.containerIDs)
	case constants.ConnectedMsg:
		err = h.engine.Connected(msg.validatorID)
	case constants.DisconnectedMsg:
		err = h.engine.Disconnected(msg.validatorID)
	}
	endTime := h.clock.Time()
	timeConsumed := endTime.Sub(startTime)
	histogram := h.metrics.getMSGHistogram(msg.messageType)
	histogram.Observe(float64(timeConsumed))
	h.cpuTracker.UtilizeTime(msg.validatorID, startTime, endTime)
	return err
}

// GetAcceptedFrontier passes a GetAcceptedFrontier message received from the
// network to the consensus engine.
func (h *Handler) GetAcceptedFrontier(
	validatorID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	onDoneHandling func(),
) {
	h.unprocessedMsgs.Push(message{
		messageType:    constants.GetAcceptedFrontierMsg,
		validatorID:    validatorID,
		requestID:      requestID,
		deadline:       deadline,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// AcceptedFrontier passes a AcceptedFrontier message received from the network
// to the consensus engine.
func (h *Handler) AcceptedFrontier(
	validatorID ids.ShortID,
	requestID uint32,
	containerIDs []ids.ID,
	onDoneHandling func(),
) {
	h.unprocessedMsgs.Push(message{
		messageType:    constants.AcceptedFrontierMsg,
		validatorID:    validatorID,
		requestID:      requestID,
		containerIDs:   containerIDs,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// GetAcceptedFrontierFailed passes a GetAcceptedFrontierFailed message received
// from the network to the consensus engine.
func (h *Handler) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) {
	// TODO mark this as reliable?
	h.unprocessedMsgs.Push(message{
		messageType: constants.GetAcceptedFrontierFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	})
}

// GetAccepted passes a GetAccepted message received from the
// network to the consensus engine.
func (h *Handler) GetAccepted(
	validatorID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	containerIDs []ids.ID,
	onDoneHandling func(),
) {
	h.unprocessedMsgs.Push(message{
		messageType:    constants.GetAcceptedMsg,
		validatorID:    validatorID,
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
	validatorID ids.ShortID,
	requestID uint32,
	containerIDs []ids.ID,
	onDoneHandling func(),
) {
	h.unprocessedMsgs.Push(message{
		messageType:    constants.AcceptedMsg,
		validatorID:    validatorID,
		requestID:      requestID,
		containerIDs:   containerIDs,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// GetAcceptedFailed passes a GetAcceptedFailed message received from the
// network to the consensus engine.
func (h *Handler) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) {
	// TODO mark as reliable?
	h.unprocessedMsgs.Push(message{
		messageType: constants.GetAcceptedFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	})
}

// GetAncestors passes a GetAncestors message received from the network to the consensus engine.
func (h *Handler) GetAncestors(
	validatorID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	onDoneHandling func(),
) {
	h.unprocessedMsgs.Push(message{
		messageType:    constants.GetAncestorsMsg,
		validatorID:    validatorID,
		requestID:      requestID,
		deadline:       deadline,
		containerID:    containerID,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// MultiPut passes a MultiPut message received from the network to the consensus engine.
func (h *Handler) MultiPut(
	validatorID ids.ShortID,
	requestID uint32,
	containers [][]byte,
	onDoneHandling func(),
) {
	h.unprocessedMsgs.Push(message{
		messageType:    constants.MultiPutMsg,
		validatorID:    validatorID,
		requestID:      requestID,
		containers:     containers,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// GetAncestorsFailed passes a GetAncestorsFailed message to the consensus engine.
func (h *Handler) GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) {
	// TODO mark as reliable?
	h.unprocessedMsgs.Push(message{
		messageType: constants.GetAncestorsFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	})
}

// Timeout passes a new timeout notification to the consensus engine
func (h *Handler) Timeout() {
	h.unprocessedMsgs.Push(message{
		messageType: constants.TimeoutMsg,
	})
}

// Get passes a Get message received from the network to the consensus engine.
func (h *Handler) Get(
	validatorID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	onDoneHandling func(),
) {
	h.unprocessedMsgs.Push(message{
		messageType:    constants.GetMsg,
		validatorID:    validatorID,
		requestID:      requestID,
		deadline:       deadline,
		containerID:    containerID,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// Put passes a Put message received from the network to the consensus engine.
func (h *Handler) Put(
	validatorID ids.ShortID,
	requestID uint32,
	containerID ids.ID,
	container []byte,
	onDoneHandling func(),
) {
	h.unprocessedMsgs.Push(message{
		messageType:    constants.PutMsg,
		validatorID:    validatorID,
		requestID:      requestID,
		containerID:    containerID,
		container:      container,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// GetFailed passes a GetFailed message to the consensus engine.
func (h *Handler) GetFailed(validatorID ids.ShortID, requestID uint32) {
	// TODO mark as reliable?
	h.unprocessedMsgs.Push(message{
		messageType: constants.GetFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	})
}

// PushQuery passes a PushQuery message received from the network to the consensus engine.
func (h *Handler) PushQuery(
	validatorID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	container []byte,
	onDoneHandling func(),
) {
	h.unprocessedMsgs.Push(message{
		messageType:    constants.PushQueryMsg,
		validatorID:    validatorID,
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
	validatorID ids.ShortID,
	requestID uint32,
	deadline time.Time,
	containerID ids.ID,
	onDoneHandling func(),
) {
	h.unprocessedMsgs.Push(message{
		messageType:    constants.PullQueryMsg,
		validatorID:    validatorID,
		requestID:      requestID,
		deadline:       deadline,
		containerID:    containerID,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// Chits passes a Chits message received from the network to the consensus engine.
func (h *Handler) Chits(validatorID ids.ShortID, requestID uint32, votes []ids.ID, onDoneHandling func()) {
	h.unprocessedMsgs.Push(message{
		messageType:    constants.ChitsMsg,
		validatorID:    validatorID,
		requestID:      requestID,
		containerIDs:   votes,
		received:       h.clock.Time(),
		onDoneHandling: onDoneHandling,
	})
}

// QueryFailed passes a QueryFailed message received from the network to the consensus engine.
func (h *Handler) QueryFailed(validatorID ids.ShortID, requestID uint32) {
	// TODO mark as reliable?
	h.unprocessedMsgs.Push(message{
		messageType: constants.QueryFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	})
}

// Connected passes a new connection notification to the consensus engine
func (h *Handler) Connected(validatorID ids.ShortID) {
	h.unprocessedMsgs.Push(message{
		messageType: constants.ConnectedMsg,
		validatorID: validatorID,
	})
}

// Disconnected passes a new connection notification to the consensus engine
func (h *Handler) Disconnected(validatorID ids.ShortID) {
	h.unprocessedMsgs.Push(message{
		messageType: constants.DisconnectedMsg,
		validatorID: validatorID,
	})
}

// Gossip passes a gossip request to the consensus engine
func (h *Handler) Gossip() {
	if !h.ctx.IsBootstrapped() {
		// Shouldn't send gossiping messages while the chain is bootstrapping
		return
	}
	h.unprocessedMsgs.Push(message{
		messageType: constants.GossipMsg,
	})
}

// Notify ...
func (h *Handler) Notify(msg common.Message) {
	h.unprocessedMsgs.Push(message{
		messageType:  constants.NotifyMsg,
		notification: msg,
	})
}

// StartShutdown starts the shutdown process for this handler/engine.
// [h] should not be invoked again after calling this method.
// [h.closed] is closed when done shutting down.
func (h *Handler) StartShutdown() {
	h.closing.SetValue(true)
	h.unprocessedMsgs.Shutdown()
	h.engine.Halt()
}

// Shuts down [h.engine] and calls [h.onCloseF].
func (h *Handler) shutdownDispatch() {
	h.ctx.Lock.Lock()
	defer h.ctx.Lock.Unlock()

	startTime := time.Now()
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

// func (h *Handler) sendReliableMsg(msg message) {
// 	h.reliableMsgsLock.Lock()
// 	defer h.reliableMsgsLock.Unlock()

// 	h.metrics.pending.Inc()
// 	h.reliableMsgs = append(h.reliableMsgs, msg)
// 	select {
// 	case h.reliableMsgsSignalChan <- struct{}{}:
// 	default:
// 	}
// }

func (h *Handler) endInterval() {
	now := h.clock.Time()
	h.cpuTracker.EndInterval(now)
}

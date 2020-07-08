// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/utils/timer"
)

// Handler passes incoming messages from the network to the consensus engine
// (Actually, it receives the incoming messages from a ChainRouter, but same difference)
type Handler struct {
	metrics

	msgs             chan message
	reliableMsgsSema chan struct{}
	reliableMsgsLock sync.Mutex
	reliableMsgs     []message
	closed           chan struct{}
	msgChan          <-chan common.Message

	clock              timer.Clock
	dropMessageTimeout time.Duration

	ctx    *snow.Context
	engine common.Engine

	toClose func()
	closing bool
}

// Initialize this consensus handler
func (h *Handler) Initialize(
	engine common.Engine,
	msgChan <-chan common.Message,
	bufferSize int,
	namespace string,
	metrics prometheus.Registerer,
) {
	h.metrics.Initialize(namespace, metrics)
	h.msgs = make(chan message, bufferSize)
	h.reliableMsgsSema = make(chan struct{}, 1)
	h.closed = make(chan struct{})
	h.msgChan = msgChan
	h.dropMessageTimeout = timeout.DefaultRequestTimeout

	h.ctx = engine.Context()
	h.engine = engine
}

// Context of this Handler
func (h *Handler) Context() *snow.Context { return h.engine.Context() }

// Engine returns the engine this handler dispatches to
func (h *Handler) Engine() common.Engine { return h.engine }

// SetEngine sets the engine this handler dispatches to
func (h *Handler) SetEngine(engine common.Engine) { h.engine = engine }

// Dispatch waits for incoming messages from the network
// and, when they arrive, sends them to the consensus engine
func (h *Handler) Dispatch() {
	defer func() {
		h.ctx.Log.Info("finished shutting down chain")
		close(h.closed)
	}()

	for {
		select {
		case msg, ok := <-h.msgs:
			if !ok {
				// the msgs channel has been closed, so this dispatcher should exit
				return
			}

			if !msg.deadline.IsZero() && h.clock.Time().After(msg.deadline) {
				h.ctx.Log.Verbo("Dropping message due to likely timeout: %s", msg)
				h.metrics.pending.Dec()
				h.metrics.dropped.Inc()
				continue
			}

			h.metrics.pending.Dec()
			h.dispatchMsg(msg)
		case <-h.reliableMsgsSema:
			// get all the reliable messages
			h.reliableMsgsLock.Lock()
			msgs := h.reliableMsgs
			h.reliableMsgs = nil
			h.reliableMsgsLock.Unlock()

			// fire all the reliable messages
			for _, msg := range msgs {
				h.metrics.pending.Dec()
				h.dispatchMsg(msg)
			}
		case msg := <-h.msgChan:
			// handle a message from the VM
			h.dispatchMsg(message{messageType: notifyMsg, notification: msg})
		}
		if h.closing && h.toClose != nil {
			go h.toClose()
		}
	}
}

// Dispatch a message to the consensus engine.
// Returns true iff this consensus handler (and its associated engine) should shutdown
// (due to receipt of a shutdown message)
func (h *Handler) dispatchMsg(msg message) {
	if h.closing {
		h.ctx.Log.Debug("dropping message due to closing:\n%s", msg)
		h.metrics.dropped.Inc()
		return
	}

	startTime := h.clock.Time()

	h.ctx.Lock.Lock()
	defer h.ctx.Lock.Unlock()

	h.ctx.Log.Verbo("Forwarding message to consensus: %s", msg)
	var (
		err  error
		done bool
	)
	switch msg.messageType {
	case getAcceptedFrontierMsg:
		err = h.engine.GetAcceptedFrontier(msg.validatorID, msg.requestID)
		h.getAcceptedFrontier.Observe(float64(h.clock.Time().Sub(startTime)))
	case acceptedFrontierMsg:
		err = h.engine.AcceptedFrontier(msg.validatorID, msg.requestID, msg.containerIDs)
		h.acceptedFrontier.Observe(float64(h.clock.Time().Sub(startTime)))
	case getAcceptedFrontierFailedMsg:
		err = h.engine.GetAcceptedFrontierFailed(msg.validatorID, msg.requestID)
		h.getAcceptedFrontierFailed.Observe(float64(h.clock.Time().Sub(startTime)))
	case getAcceptedMsg:
		err = h.engine.GetAccepted(msg.validatorID, msg.requestID, msg.containerIDs)
		h.getAccepted.Observe(float64(h.clock.Time().Sub(startTime)))
	case acceptedMsg:
		err = h.engine.Accepted(msg.validatorID, msg.requestID, msg.containerIDs)
		h.accepted.Observe(float64(h.clock.Time().Sub(startTime)))
	case getAcceptedFailedMsg:
		err = h.engine.GetAcceptedFailed(msg.validatorID, msg.requestID)
		h.getAcceptedFailed.Observe(float64(h.clock.Time().Sub(startTime)))
	case getAncestorsMsg:
		err = h.engine.GetAncestors(msg.validatorID, msg.requestID, msg.containerID)
		h.getAncestors.Observe(float64(h.clock.Time().Sub(startTime)))
	case getAncestorsFailedMsg:
		err = h.engine.GetAncestorsFailed(msg.validatorID, msg.requestID)
		h.getAncestorsFailed.Observe(float64(h.clock.Time().Sub(startTime)))
	case multiPutMsg:
		err = h.engine.MultiPut(msg.validatorID, msg.requestID, msg.containers)
		h.multiPut.Observe(float64(h.clock.Time().Sub(startTime)))
	case getMsg:
		err = h.engine.Get(msg.validatorID, msg.requestID, msg.containerID)
		h.get.Observe(float64(h.clock.Time().Sub(startTime)))
	case getFailedMsg:
		err = h.engine.GetFailed(msg.validatorID, msg.requestID)
		h.getFailed.Observe(float64(h.clock.Time().Sub(startTime)))
	case putMsg:
		err = h.engine.Put(msg.validatorID, msg.requestID, msg.containerID, msg.container)
		h.put.Observe(float64(h.clock.Time().Sub(startTime)))
	case pushQueryMsg:
		err = h.engine.PushQuery(msg.validatorID, msg.requestID, msg.containerID, msg.container)
		h.pushQuery.Observe(float64(h.clock.Time().Sub(startTime)))
	case pullQueryMsg:
		err = h.engine.PullQuery(msg.validatorID, msg.requestID, msg.containerID)
		h.pullQuery.Observe(float64(h.clock.Time().Sub(startTime)))
	case queryFailedMsg:
		err = h.engine.QueryFailed(msg.validatorID, msg.requestID)
		h.queryFailed.Observe(float64(h.clock.Time().Sub(startTime)))
	case chitsMsg:
		err = h.engine.Chits(msg.validatorID, msg.requestID, msg.containerIDs)
		h.chits.Observe(float64(h.clock.Time().Sub(startTime)))
	case notifyMsg:
		err = h.engine.Notify(msg.notification)
		h.notify.Observe(float64(h.clock.Time().Sub(startTime)))
	case gossipMsg:
		err = h.engine.Gossip()
		h.gossip.Observe(float64(h.clock.Time().Sub(startTime)))
	case shutdownMsg:
		err = h.engine.Shutdown()
		h.shutdown.Observe(float64(h.clock.Time().Sub(startTime)))
		done = true
	}

	if err != nil {
		h.ctx.Log.Fatal("forcing chain to shutdown due to %s", err)
	}

	h.closing = done || err != nil
}

// GetAcceptedFrontier passes a GetAcceptedFrontier message received from the
// network to the consensus engine.
func (h *Handler) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) bool {
	return h.sendMsg(message{
		messageType: getAcceptedFrontierMsg,
		validatorID: validatorID,
		requestID:   requestID,
		deadline:    h.clock.Time().Add(h.dropMessageTimeout),
	})
}

// AcceptedFrontier passes a AcceptedFrontier message received from the network
// to the consensus engine.
func (h *Handler) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) bool {
	return h.sendMsg(message{
		messageType:  acceptedFrontierMsg,
		validatorID:  validatorID,
		requestID:    requestID,
		containerIDs: containerIDs,
	})
}

// GetAcceptedFrontierFailed passes a GetAcceptedFrontierFailed message received
// from the network to the consensus engine.
func (h *Handler) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) {
	h.sendReliableMsg(message{
		messageType: getAcceptedFrontierFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	})
}

// GetAccepted passes a GetAccepted message received from the
// network to the consensus engine.
func (h *Handler) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) bool {
	return h.sendMsg(message{
		messageType:  getAcceptedMsg,
		validatorID:  validatorID,
		requestID:    requestID,
		containerIDs: containerIDs,
		deadline:     h.clock.Time().Add(h.dropMessageTimeout),
	})
}

// Accepted passes a Accepted message received from the network to the consensus
// engine.
func (h *Handler) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) bool {
	return h.sendMsg(message{
		messageType:  acceptedMsg,
		validatorID:  validatorID,
		requestID:    requestID,
		containerIDs: containerIDs,
	})
}

// GetAcceptedFailed passes a GetAcceptedFailed message received from the
// network to the consensus engine.
func (h *Handler) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) {
	h.sendReliableMsg(message{
		messageType: getAcceptedFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	})
}

// GetAncestors passes a GetAncestors message received from the network to the consensus engine.
func (h *Handler) GetAncestors(validatorID ids.ShortID, requestID uint32, containerID ids.ID) bool {
	return h.sendMsg(message{
		messageType: getAncestorsMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: containerID,
		deadline:    h.clock.Time().Add(h.dropMessageTimeout),
	})
}

// MultiPut passes a MultiPut message received from the network to the consensus engine.
func (h *Handler) MultiPut(validatorID ids.ShortID, requestID uint32, containers [][]byte) bool {
	return h.sendMsg(message{
		messageType: multiPutMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containers:  containers,
	})
}

// GetAncestorsFailed passes a GetAncestorsFailed message to the consensus engine.
func (h *Handler) GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) {
	h.sendReliableMsg(message{
		messageType: getAncestorsFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	})
}

// Get passes a Get message received from the network to the consensus engine.
func (h *Handler) Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID) bool {
	return h.sendMsg(message{
		messageType: getMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: containerID,
		deadline:    h.clock.Time().Add(h.dropMessageTimeout),
	})
}

// Put passes a Put message received from the network to the consensus engine.
func (h *Handler) Put(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) bool {
	return h.sendMsg(message{
		messageType: putMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: containerID,
		container:   container,
	})
}

// GetFailed passes a GetFailed message to the consensus engine.
func (h *Handler) GetFailed(validatorID ids.ShortID, requestID uint32) {
	h.sendReliableMsg(message{
		messageType: getFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	})
}

// PushQuery passes a PushQuery message received from the network to the consensus engine.
func (h *Handler) PushQuery(validatorID ids.ShortID, requestID uint32, blockID ids.ID, block []byte) bool {
	return h.sendMsg(message{
		messageType: pushQueryMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: blockID,
		container:   block,
		deadline:    h.clock.Time().Add(h.dropMessageTimeout),
	})
}

// PullQuery passes a PullQuery message received from the network to the consensus engine.
func (h *Handler) PullQuery(validatorID ids.ShortID, requestID uint32, blockID ids.ID) bool {
	return h.sendMsg(message{
		messageType: pullQueryMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: blockID,
		deadline:    h.clock.Time().Add(h.dropMessageTimeout),
	})
}

// Chits passes a Chits message received from the network to the consensus engine.
func (h *Handler) Chits(validatorID ids.ShortID, requestID uint32, votes ids.Set) bool {
	return h.sendMsg(message{
		messageType:  chitsMsg,
		validatorID:  validatorID,
		requestID:    requestID,
		containerIDs: votes,
	})
}

// QueryFailed passes a QueryFailed message received from the network to the consensus engine.
func (h *Handler) QueryFailed(validatorID ids.ShortID, requestID uint32) {
	h.sendReliableMsg(message{
		messageType: queryFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	})
}

// Gossip passes a gossip request to the consensus engine
func (h *Handler) Gossip() bool {
	return h.sendMsg(message{
		messageType: gossipMsg,
	})
}

// Notify ...
func (h *Handler) Notify(msg common.Message) bool {
	return h.sendMsg(message{
		messageType:  notifyMsg,
		notification: msg,
	})
}

// Shutdown shuts down the dispatcher
func (h *Handler) Shutdown() {
	h.sendReliableMsg(message{
		messageType: shutdownMsg,
	})
}

func (h *Handler) sendMsg(msg message) bool {
	select {
	case h.msgs <- msg:
		h.metrics.pending.Inc()
		return true
	default:
		h.metrics.dropped.Inc()
		return false
	}
}

func (h *Handler) sendReliableMsg(msg message) {
	h.reliableMsgsLock.Lock()
	defer h.reliableMsgsLock.Unlock()

	h.metrics.pending.Inc()
	h.reliableMsgs = append(h.reliableMsgs, msg)
	select {
	case h.reliableMsgsSema <- struct{}{}:
	default:
	}
}

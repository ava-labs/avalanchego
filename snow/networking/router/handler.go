// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/prometheus/client_golang/prometheus"
)

// Handler passes incoming messages from the network to the consensus engine
// (Actually, it receives the incoming messages from a ChainRouter, but same difference)
type Handler struct {
	metrics

	msgs    chan message
	closed  chan struct{}
	engine  common.Engine
	msgChan <-chan common.Message

	toClose func()
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
	h.closed = make(chan struct{})
	h.engine = engine
	h.msgChan = msgChan
}

// Context of this Handler
func (h *Handler) Context() *snow.Context { return h.engine.Context() }

// Dispatch waits for incoming messages from the network
// and, when they arrive, sends them to the consensus engine
func (h *Handler) Dispatch() {
	log := h.Context().Log
	defer func() {
		log.Info("finished shutting down chain")
		close(h.closed)
	}()

	closing := false
	for {
		select {
		case msg, ok := <-h.msgs:
			if !ok {
				return
			}
			h.metrics.pending.Dec()
			if closing {
				log.Debug("dropping message due to closing:\n%s", msg)
				continue
			}
			if h.dispatchMsg(msg) {
				closing = true
			}
		case msg := <-h.msgChan:
			if closing {
				log.Debug("dropping internal message due to closing:\n%s", msg)
				continue
			}
			if h.dispatchMsg(message{messageType: notifyMsg, notification: msg}) {
				closing = true
			}
		}
		if closing && h.toClose != nil {
			go h.toClose()
		}
	}
}

// Dispatch a message to the consensus engine.
// Returns true iff this consensus handler (and its associated engine) should shutdown
// (due to receipt of a shutdown message)
func (h *Handler) dispatchMsg(msg message) bool {
	startTime := time.Now()
	ctx := h.engine.Context()

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	ctx.Log.Verbo("Forwarding message to consensus: %s", msg)
	var (
		err  error
		done bool
	)
	switch msg.messageType {
	case getAcceptedFrontierMsg:
		err = h.engine.GetAcceptedFrontier(msg.validatorID, msg.requestID)
		h.getAcceptedFrontier.Observe(float64(time.Now().Sub(startTime)))
	case acceptedFrontierMsg:
		err = h.engine.AcceptedFrontier(msg.validatorID, msg.requestID, msg.containerIDs)
		h.acceptedFrontier.Observe(float64(time.Now().Sub(startTime)))
	case getAcceptedFrontierFailedMsg:
		err = h.engine.GetAcceptedFrontierFailed(msg.validatorID, msg.requestID)
		h.getAcceptedFrontierFailed.Observe(float64(time.Now().Sub(startTime)))
	case getAcceptedMsg:
		err = h.engine.GetAccepted(msg.validatorID, msg.requestID, msg.containerIDs)
		h.getAccepted.Observe(float64(time.Now().Sub(startTime)))
	case acceptedMsg:
		err = h.engine.Accepted(msg.validatorID, msg.requestID, msg.containerIDs)
		h.accepted.Observe(float64(time.Now().Sub(startTime)))
	case getAcceptedFailedMsg:
		err = h.engine.GetAcceptedFailed(msg.validatorID, msg.requestID)
		h.getAcceptedFailed.Observe(float64(time.Now().Sub(startTime)))
	case getAncestorsMsg:
		err = h.engine.GetAncestors(msg.validatorID, msg.requestID, msg.containerID)
		h.getAncestors.Observe(float64(time.Now().Sub(startTime)))
	case getAncestorsFailedMsg:
		err = h.engine.GetAncestorsFailed(msg.validatorID, msg.requestID)
		h.getAncestorsFailed.Observe(float64(time.Now().Sub(startTime)))
	case multiPutMsg:
		err = h.engine.MultiPut(msg.validatorID, msg.requestID, msg.containers)
		h.multiPut.Observe(float64(time.Now().Sub(startTime)))
	case getMsg:
		err = h.engine.Get(msg.validatorID, msg.requestID, msg.containerID)
		h.get.Observe(float64(time.Now().Sub(startTime)))
	case getFailedMsg:
		err = h.engine.GetFailed(msg.validatorID, msg.requestID)
		h.getFailed.Observe(float64(time.Now().Sub(startTime)))
	case putMsg:
		err = h.engine.Put(msg.validatorID, msg.requestID, msg.containerID, msg.container)
		h.put.Observe(float64(time.Now().Sub(startTime)))
	case pushQueryMsg:
		err = h.engine.PushQuery(msg.validatorID, msg.requestID, msg.containerID, msg.container)
		h.pushQuery.Observe(float64(time.Now().Sub(startTime)))
	case pullQueryMsg:
		err = h.engine.PullQuery(msg.validatorID, msg.requestID, msg.containerID)
		h.pullQuery.Observe(float64(time.Now().Sub(startTime)))
	case queryFailedMsg:
		err = h.engine.QueryFailed(msg.validatorID, msg.requestID)
		h.queryFailed.Observe(float64(time.Now().Sub(startTime)))
	case chitsMsg:
		err = h.engine.Chits(msg.validatorID, msg.requestID, msg.containerIDs)
		h.chits.Observe(float64(time.Now().Sub(startTime)))
	case notifyMsg:
		err = h.engine.Notify(msg.notification)
		h.notify.Observe(float64(time.Now().Sub(startTime)))
	case gossipMsg:
		err = h.engine.Gossip()
		h.gossip.Observe(float64(time.Now().Sub(startTime)))
	case shutdownMsg:
		err = h.engine.Shutdown()
		h.shutdown.Observe(float64(time.Now().Sub(startTime)))
		done = true
	}

	if err != nil {
		ctx.Log.Fatal("forcing chain to shutdown due to %s", err)
	}
	return done || err != nil
}

// GetAcceptedFrontier passes a GetAcceptedFrontier message received from the
// network to the consensus engine.
func (h *Handler) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: getAcceptedFrontierMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// AcceptedFrontier passes a AcceptedFrontier message received from the network
// to the consensus engine.
func (h *Handler) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType:  acceptedFrontierMsg,
		validatorID:  validatorID,
		requestID:    requestID,
		containerIDs: containerIDs,
	}
}

// GetAcceptedFrontierFailed passes a GetAcceptedFrontierFailed message received
// from the network to the consensus engine.
func (h *Handler) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: getAcceptedFrontierFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// GetAccepted passes a GetAccepted message received from the
// network to the consensus engine.
func (h *Handler) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType:  getAcceptedMsg,
		validatorID:  validatorID,
		requestID:    requestID,
		containerIDs: containerIDs,
	}
}

// Accepted passes a Accepted message received from the network to the consensus
// engine.
func (h *Handler) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType:  acceptedMsg,
		validatorID:  validatorID,
		requestID:    requestID,
		containerIDs: containerIDs,
	}
}

// GetAcceptedFailed passes a GetAcceptedFailed message received from the
// network to the consensus engine.
func (h *Handler) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: getAcceptedFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// Get passes a Get message received from the network to the consensus engine.
func (h *Handler) Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: getMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: containerID,
	}
}

// GetAncestors passes a GetAncestors message received from the network to the consensus engine.
func (h *Handler) GetAncestors(validatorID ids.ShortID, requestID uint32, containerID ids.ID) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: getAncestorsMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: containerID,
	}
}

// Put passes a Put message received from the network to the consensus engine.
func (h *Handler) Put(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: putMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: containerID,
		container:   container,
	}
}

// MultiPut passes a MultiPut message received from the network to the consensus engine.
func (h *Handler) MultiPut(validatorID ids.ShortID, requestID uint32, containers [][]byte) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: multiPutMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containers:  containers,
	}
}

// GetFailed passes a GetFailed message to the consensus engine.
func (h *Handler) GetFailed(validatorID ids.ShortID, requestID uint32) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: getFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// GetAncestorsFailed passes a GetAncestorsFailed message to the consensus engine.
func (h *Handler) GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: getAncestorsFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// PushQuery passes a PushQuery message received from the network to the consensus engine.
func (h *Handler) PushQuery(validatorID ids.ShortID, requestID uint32, blockID ids.ID, block []byte) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: pushQueryMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: blockID,
		container:   block,
	}
}

// PullQuery passes a PullQuery message received from the network to the consensus engine.
func (h *Handler) PullQuery(validatorID ids.ShortID, requestID uint32, blockID ids.ID) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: pullQueryMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: blockID,
	}
}

// Chits passes a Chits message received from the network to the consensus engine.
func (h *Handler) Chits(validatorID ids.ShortID, requestID uint32, votes ids.Set) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType:  chitsMsg,
		validatorID:  validatorID,
		requestID:    requestID,
		containerIDs: votes,
	}
}

// QueryFailed passes a QueryFailed message received from the network to the consensus engine.
func (h *Handler) QueryFailed(validatorID ids.ShortID, requestID uint32) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType: queryFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// Gossip passes a gossip request to the consensus engine
func (h *Handler) Gossip() {
	h.metrics.pending.Inc()
	h.msgs <- message{messageType: gossipMsg}
}

// Shutdown shuts down the dispatcher
func (h *Handler) Shutdown() {
	h.metrics.pending.Inc()
	h.msgs <- message{messageType: shutdownMsg}
}

// Notify ...
func (h *Handler) Notify(msg common.Message) {
	h.metrics.pending.Inc()
	h.msgs <- message{
		messageType:  notifyMsg,
		notification: msg,
	}
}

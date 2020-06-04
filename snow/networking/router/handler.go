// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
)

// Handler passes incoming messages from the network to the consensus engine
// (Actually, it receives the incoming messages from a ChainRouter, but same difference)
type Handler struct {
	msgs    chan message
	closed  chan struct{}
	engine  common.Engine
	msgChan <-chan common.Message

	toClose func()
}

// Initialize this consensus handler
func (h *Handler) Initialize(engine common.Engine, msgChan <-chan common.Message, bufferSize int) {
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
	case acceptedFrontierMsg:
		err = h.engine.AcceptedFrontier(msg.validatorID, msg.requestID, msg.containerIDs)
	case getAcceptedFrontierFailedMsg:
		err = h.engine.GetAcceptedFrontierFailed(msg.validatorID, msg.requestID)
	case getAcceptedMsg:
		err = h.engine.GetAccepted(msg.validatorID, msg.requestID, msg.containerIDs)
	case acceptedMsg:
		err = h.engine.Accepted(msg.validatorID, msg.requestID, msg.containerIDs)
	case getAcceptedFailedMsg:
		err = h.engine.GetAcceptedFailed(msg.validatorID, msg.requestID)
	case getMsg:
		err = h.engine.Get(msg.validatorID, msg.requestID, msg.containerID)
	case getAncestorsMsg:
		err = h.engine.GetAncestors(msg.validatorID, msg.requestID, msg.containerID)
	case getFailedMsg:
		err = h.engine.GetFailed(msg.validatorID, msg.requestID)
	case getAncestorsFailedMsg:
		err = h.engine.GetAncestorsFailed(msg.validatorID, msg.requestID)
	case putMsg:
		err = h.engine.Put(msg.validatorID, msg.requestID, msg.containerID, msg.container)
	case multiPutMsg:
		err = h.engine.MultiPut(msg.validatorID, msg.requestID, msg.containers)
	case pushQueryMsg:
		err = h.engine.PushQuery(msg.validatorID, msg.requestID, msg.containerID, msg.container)
	case pullQueryMsg:
		err = h.engine.PullQuery(msg.validatorID, msg.requestID, msg.containerID)
	case queryFailedMsg:
		err = h.engine.QueryFailed(msg.validatorID, msg.requestID)
	case chitsMsg:
		err = h.engine.Chits(msg.validatorID, msg.requestID, msg.containerIDs)
	case notifyMsg:
		err = h.engine.Notify(msg.notification)
	case gossipMsg:
		err = h.engine.Gossip()
	case shutdownMsg:
		err = h.engine.Shutdown()
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
	h.msgs <- message{
		messageType: getAcceptedFrontierMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// AcceptedFrontier passes a AcceptedFrontier message received from the network
// to the consensus engine.
func (h *Handler) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
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
	h.msgs <- message{
		messageType: getAcceptedFrontierFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// GetAccepted passes a GetAccepted message received from the
// network to the consensus engine.
func (h *Handler) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
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
	h.msgs <- message{
		messageType: getAcceptedFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// Get passes a Get message received from the network to the consensus engine.
func (h *Handler) Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID) {
	h.msgs <- message{
		messageType: getMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: containerID,
	}
}

// GetAncestors passes a GetAncestors message received from the network to the consensus engine.
func (h *Handler) GetAncestors(validatorID ids.ShortID, requestID uint32, containerID ids.ID) {
	h.msgs <- message{
		messageType: getAncestorsMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: containerID,
	}
}

// Put passes a Put message received from the network to the consensus engine.
func (h *Handler) Put(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) {
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
	h.msgs <- message{
		messageType: multiPutMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containers:  containers,
	}
}

// GetFailed passes a GetFailed message to the consensus engine.
func (h *Handler) GetFailed(validatorID ids.ShortID, requestID uint32) {
	h.msgs <- message{
		messageType: getFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// GetAncestorsFailed passes a GetAncestorsFailed message to the consensus engine.
func (h *Handler) GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) {
	h.msgs <- message{
		messageType: getAncestorsFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// PushQuery passes a PushQuery message received from the network to the consensus engine.
func (h *Handler) PushQuery(validatorID ids.ShortID, requestID uint32, blockID ids.ID, block []byte) {
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
	h.msgs <- message{
		messageType: pullQueryMsg,
		validatorID: validatorID,
		requestID:   requestID,
		containerID: blockID,
	}
}

// Chits passes a Chits message received from the network to the consensus engine.
func (h *Handler) Chits(validatorID ids.ShortID, requestID uint32, votes ids.Set) {
	h.msgs <- message{
		messageType:  chitsMsg,
		validatorID:  validatorID,
		requestID:    requestID,
		containerIDs: votes,
	}
}

// QueryFailed passes a QueryFailed message received from the network to the consensus engine.
func (h *Handler) QueryFailed(validatorID ids.ShortID, requestID uint32) {
	h.msgs <- message{
		messageType: queryFailedMsg,
		validatorID: validatorID,
		requestID:   requestID,
	}
}

// Gossip passes a gossip request to the consensus engine
func (h *Handler) Gossip() { h.msgs <- message{messageType: gossipMsg} }

// Shutdown shuts down the dispatcher
func (h *Handler) Shutdown() { h.msgs <- message{messageType: shutdownMsg} }

// Notify ...
func (h *Handler) Notify(msg common.Message) {
	h.msgs <- message{
		messageType:  notifyMsg,
		notification: msg,
	}
}

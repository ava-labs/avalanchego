// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

var _ Handler = &MsgHandlerNoOps{}

func NewMsgHandlerNoOps(ctx *snow.ConsensusContext) MsgHandlerNoOps {
	return MsgHandlerNoOps{
		log: ctx.Log,
	}
}

type MsgHandlerNoOps struct {
	log logging.Logger
}

// FrontierHandler interface
func (nop MsgHandlerNoOps) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) error {
	nop.log.Debug("GetAcceptedFrontier(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.log.Debug("AcceptedFrontier(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.log.Debug("GetAcceptedFrontierFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

// AcceptedHandler inteface
func (nop MsgHandlerNoOps) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.log.Debug("GetAccepted(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.log.Debug("Accepted(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.log.Debug("GetAcceptedFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

// AppHandler interface
func (nop MsgHandlerNoOps) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	nop.log.Debug("AppRequest(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	nop.log.Debug("AppRequestFailed(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	nop.log.Debug("AppResponse(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) AppGossip(nodeID ids.ShortID, msg []byte) error {
	nop.log.Debug("AppGossip(%s) unhandled by this gear. Dropped.", nodeID)
	return nil
}

// FetchHandler interface
func (nop MsgHandlerNoOps) Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error {
	nop.log.Debug("Get(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) GetAncestors(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error {
	nop.log.Debug("GetAncestors(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) Put(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	if requestID == constants.GossipMsgRequestID {
		nop.log.Verbo("Gossip Put(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	} else {
		nop.log.Debug("Put(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	}
	return nil
}

func (nop MsgHandlerNoOps) MultiPut(validatorID ids.ShortID, requestID uint32, containers [][]byte) error {
	nop.log.Debug("MultiPut(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) GetFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.log.Debug("GetFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.log.Debug("GetAncestorsFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

// InternalHandler interface
func (nop MsgHandlerNoOps) Gossip() error {
	nop.log.Debug("Gossip unhandled by this gear. Dropped.")
	return nil
}

func (nop MsgHandlerNoOps) Timeout() error {
	nop.log.Debug("Timeout unhandled by this gear. Dropped.")
	return nil
}

func (nop MsgHandlerNoOps) Halt() {
	nop.log.Debug("Halt unhandled by this gear. Dropped.")
}

func (nop MsgHandlerNoOps) Shutdown() error {
	nop.log.Debug("Shutdown unhandled by this gear. Dropped.")
	return nil
}

func (nop MsgHandlerNoOps) Notify(msg Message) error {
	nop.log.Debug("Notify message %s unhandled by this gear. Dropped", msg.String())
	return nil
}

func (nop MsgHandlerNoOps) Connected(validatorID ids.ShortID, nodeVersion version.Application) error {
	nop.log.Debug("Connected(%s) unhandled by this gear. Dropped.", validatorID)
	return nil
}

func (nop MsgHandlerNoOps) Disconnected(validatorID ids.ShortID) error {
	nop.log.Debug("Disconnected(%s) unhandled by this gear. Dropped.", validatorID)
	return nil
}

// QueryHandler interface
func (nop MsgHandlerNoOps) PullQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID) error {
	nop.log.Debug("PullQuery(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	return nil
}

func (nop MsgHandlerNoOps) PushQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	nop.log.Debug("PushQuery(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	return nil
}

func (nop MsgHandlerNoOps) Chits(vdr ids.ShortID, requestID uint32, votes []ids.ID) error {
	nop.log.Debug("Chits(%s, %d) unhandled by this gear. Dropped.", vdr, requestID)
	return nil
}

func (nop MsgHandlerNoOps) QueryFailed(vdr ids.ShortID, requestID uint32) error {
	nop.log.Debug("QueryFailed(%s, %d) unhandled by this gear. Dropped.", vdr, requestID)
	return nil
}

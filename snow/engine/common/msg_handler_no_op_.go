// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var _ Handler = &MsgHandlerNoOps{}

func NewMsgHandlerNoOps(ctx *snow.ConsensusContext) MsgHandlerNoOps {
	return MsgHandlerNoOps{
		ctx: ctx,
	}
}

type MsgHandlerNoOps struct {
	ctx *snow.ConsensusContext
}

// FrontierHandler interface
func (nop MsgHandlerNoOps) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) error {
	nop.ctx.Log.Debug("GetAcceptedFrontier(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.ctx.Log.Debug("AcceptedFrontier(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.ctx.Log.Debug("GetAcceptedFrontierFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

// AcceptedHandler inteface
func (nop MsgHandlerNoOps) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.ctx.Log.Debug("GetAccepted(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.ctx.Log.Debug("Accepted(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.ctx.Log.Debug("GetAcceptedFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

// AppHandler interface
func (nop MsgHandlerNoOps) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	nop.ctx.Log.Debug("AppRequest(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	nop.ctx.Log.Debug("AppRequestFailed(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	nop.ctx.Log.Debug("AppResponse(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) AppGossip(nodeID ids.ShortID, msg []byte) error {
	nop.ctx.Log.Debug("AppGossip(%s) unhandled by this gear. Dropped.", nodeID)
	return nil
}

// FetchHandler interface
func (nop MsgHandlerNoOps) Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error {
	nop.ctx.Log.Debug("Get(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) GetAncestors(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error {
	nop.ctx.Log.Debug("GetAncestors(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) Put(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	if requestID == constants.GossipMsgRequestID {
		nop.ctx.Log.Verbo("Gossip Put(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	} else {
		nop.ctx.Log.Debug("Put(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	}
	return nil
}

func (nop MsgHandlerNoOps) MultiPut(validatorID ids.ShortID, requestID uint32, containers [][]byte) error {
	nop.ctx.Log.Debug("MultiPut(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) GetFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.ctx.Log.Debug("GetFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop MsgHandlerNoOps) GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.ctx.Log.Debug("GetAncestorsFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

// InternalHandler interface
func (nop MsgHandlerNoOps) Gossip() error {
	nop.ctx.Log.Debug("Gossip unhandled by this gear. Dropped.")
	return nil
}

func (nop MsgHandlerNoOps) Timeout() error {
	nop.ctx.Log.Debug("Timeout unhandled by this gear. Dropped.")
	return nil
}

func (nop MsgHandlerNoOps) Halt() {
	nop.ctx.Log.Debug("Halt unhandled by this gear. Dropped.")
}

func (nop MsgHandlerNoOps) Shutdown() error {
	nop.ctx.Log.Debug("Shutdown unhandled by this gear. Dropped.")
	return nil
}

func (nop MsgHandlerNoOps) Notify(Message) error {
	nop.ctx.Log.Debug("Notify unhandled by this gear. Dropped.")
	return nil
}

func (nop MsgHandlerNoOps) Connected(validatorID ids.ShortID) error {
	nop.ctx.Log.Debug("Connected(%s) unhandled by this gear. Dropped.", validatorID)
	return nil
}

func (nop MsgHandlerNoOps) Disconnected(validatorID ids.ShortID) error {
	nop.ctx.Log.Debug("Disconnected(%s) unhandled by this gear. Dropped.", validatorID)
	return nil
}

// QueryHandler interface
func (nop MsgHandlerNoOps) PullQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID) error {
	nop.ctx.Log.Debug("PullQuery(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	return nil
}

func (nop MsgHandlerNoOps) PushQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	nop.ctx.Log.Debug("PushQuery(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	return nil
}

func (nop MsgHandlerNoOps) Chits(vdr ids.ShortID, requestID uint32, votes []ids.ID) error {
	nop.ctx.Log.Debug("Chits(%s, %d) unhandled by this gear. Dropped.", vdr, requestID)
	return nil
}

func (nop MsgHandlerNoOps) QueryFailed(vdr ids.ShortID, requestID uint32) error {
	nop.ctx.Log.Debug("QueryFailed(%s, %d) unhandled by this gear. Dropped.", vdr, requestID)
	return nil
}

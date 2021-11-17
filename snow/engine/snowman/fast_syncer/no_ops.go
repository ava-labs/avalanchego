// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowsyncer

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// EngineNoOps lists all messages that are dropped during nomal operation phase
// Whenever we drop a message, we do that raising an error (unlike BootstrapNoOps)

type FastSyncNoOps struct {
	Ctx *snow.ConsensusContext
}

func (nop *FastSyncNoOps) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetAcceptedFrontier(%s, %d) unhandled during fast sync. Dropped.", validatorID, requestID)
	return nil
}

func (nop *FastSyncNoOps) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.Ctx.Log.Debug("AcceptedFrontier(%s, %d) unhandled during fast sync. Dropped.", validatorID, requestID)
	return nil
}

func (nop *FastSyncNoOps) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetAcceptedFrontierFailed(%s, %d) unhandled during fast sync. Dropped.", validatorID, requestID)
	return nil
}

func (nop *FastSyncNoOps) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.Ctx.Log.Debug("GetAccepted(%s, %d) unhandled during fast sync. Dropped.", validatorID, requestID)
	return nil
}

func (nop *FastSyncNoOps) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.Ctx.Log.Debug("Accepted(%s, %d) unhandled during fast sync. Dropped.", validatorID, requestID)
	return nil
}

func (nop *FastSyncNoOps) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetAcceptedFailed(%s, %d) unhandled during fast sync. Dropped.", validatorID, requestID)
	return nil
}

func (nop *FastSyncNoOps) GetAncestors(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error {
	nop.Ctx.Log.Debug("GetAncestors(%s, %d) unhandled during fast sync. Dropped.", validatorID, requestID)
	return nil
}

func (nop *FastSyncNoOps) MultiPut(validatorID ids.ShortID, requestID uint32, containers [][]byte) error {
	nop.Ctx.Log.Debug("MultiPut(%s, %d) unhandled during fast sync. Dropped.", validatorID, requestID)
	return nil
}

func (nop *FastSyncNoOps) GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetAncestorsFailed(%s, %d) unhandled during fast sync. Dropped.", validatorID, requestID)
	return nil
}

func (nop *FastSyncNoOps) Halt() {
	nop.Ctx.Log.Debug("Halt unhandled during fast sync. Dropped.")
}

func (nop *FastSyncNoOps) Timeout() error {
	nop.Ctx.Log.Debug("Timeout unhandled during fast sync. Dropped.")
	return nil
}

func (nop *FastSyncNoOps) Connected(validatorID ids.ShortID) error {
	nop.Ctx.Log.Debug("Connected(%s) unhandled during fast sync. Dropped.", validatorID)
	return nil
}

func (nop *FastSyncNoOps) Disconnected(validatorID ids.ShortID) error {
	nop.Ctx.Log.Debug("Disconnected(%s) unhandled during fast sync. Dropped.", validatorID)
	return nil
}

func (nop *FastSyncNoOps) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	nop.Ctx.Log.Debug("AppRequest(%s, %d) unhandled during fast sync. Dropped.", nodeID, requestID)
	return nil
}

func (nop *FastSyncNoOps) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	nop.Ctx.Log.Debug("AppResponse(%s, %d) unhandled during fast sync. Dropped.", nodeID, requestID)
	return nil
}

func (nop *FastSyncNoOps) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("AppRequestFailed(%s, %d) unhandled during fast sync. Dropped.", nodeID, requestID)
	return nil
}

func (nop *FastSyncNoOps) AppGossip(nodeID ids.ShortID, msg []byte) error {
	nop.Ctx.Log.Debug("AppGossip(%s) unhandled during fast sync. Dropped.", nodeID)
	return nil
}

func (nop *FastSyncNoOps) Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error {
	nop.Ctx.Log.Debug("Received Get message from (%s) during bootstrap. Dropping it", validatorID)
	return nil
}

func (nop *FastSyncNoOps) Put(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	if requestID == constants.GossipMsgRequestID {
		nop.Ctx.Log.Verbo("gossip Put(%s, %d, %s) unhandled during fast sync. Dropped.", vdr, requestID, blkID)
	} else {
		nop.Ctx.Log.Debug("Put(%s, %d, %s) unhandled during fast sync. Dropped.", vdr, requestID, blkID)
	}
	return nil
}

func (nop *FastSyncNoOps) GetFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetFailed(%s, %d) unhandled during fast sync. Dropped.", validatorID, requestID)
	return nil
}

func (nop *FastSyncNoOps) Gossip() error {
	nop.Ctx.Log.Debug("No Gossip during bootstrap. Dropping it")
	return nil
}

func (nop *FastSyncNoOps) Notify(common.Message) error {
	nop.Ctx.Log.Debug("Notify unhandled during fast sync. Dropped.")
	return nil
}

func (nop *FastSyncNoOps) Shutdown() error {
	nop.Ctx.Log.Debug("Called Shutdown during bootstrap. Doing nothing for now")
	return nil
}

func (nop *FastSyncNoOps) PullQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID) error {
	nop.Ctx.Log.Debug("PullQuery(%s, %d, %s) unhandled during fast sync. Dropped.", vdr, requestID, blkID)
	return nil
}

func (nop *FastSyncNoOps) PushQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	nop.Ctx.Log.Debug("PushQuery(%s, %d, %s) unhandled during fast sync. Dropped.", vdr, requestID, blkID)
	return nil
}

func (nop *FastSyncNoOps) Chits(vdr ids.ShortID, requestID uint32, votes []ids.ID) error {
	nop.Ctx.Log.Debug("Chits(%s, %d) unhandled during fast sync. Dropped.", vdr, requestID)
	return nil
}

func (nop *FastSyncNoOps) QueryFailed(vdr ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("QueryFailed(%s, %d) unhandled during fast sync. Dropped.", vdr, requestID)
	return nil
}

func (nop *FastSyncNoOps) HealthCheck() (interface{}, error) {
	return nil, nil
}

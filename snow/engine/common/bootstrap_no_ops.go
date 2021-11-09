// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// BootstrapNoOps lists all messages that are dropped during bootstrapping
// Whenever we drop a message, we do that without raising an error (unlike EngineNoOps)

type BootstrapNoOps struct {
	Ctx *snow.Context
}

func (nop *BootstrapNoOps) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	nop.Ctx.Log.Debug("AppRequest(%s, %d) unhandled by bootstrapper. Dropped.", nodeID, requestID)
	return nil
}

func (nop *BootstrapNoOps) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	nop.Ctx.Log.Debug("AppResponse(%s, %d) unhandled by bootstrapper. Dropped.", nodeID, requestID)
	return nil
}

func (nop *BootstrapNoOps) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("AppRequestFailed(%s, %d) unhandled by bootstrapper. Dropped.", nodeID, requestID)
	return nil
}

func (nop *BootstrapNoOps) AppGossip(nodeID ids.ShortID, msg []byte) error {
	nop.Ctx.Log.Debug("AppGossip(%s) unhandled by bootstrapper. Dropped.", nodeID)
	return nil
}

func (nop *BootstrapNoOps) Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID) error {
	nop.Ctx.Log.Debug("Received Get message from (%s) during bootstrap. Dropping it", validatorID)
	return nil
}

func (nop *BootstrapNoOps) Put(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	if requestID == constants.GossipMsgRequestID {
		nop.Ctx.Log.Verbo("gossip Put(%s, %d, %s) unhandled by bootstrapper. Dropped.", vdr, requestID, blkID)
	} else {
		nop.Ctx.Log.Debug("Put(%s, %d, %s) unhandled by bootstrapper. Dropped.", vdr, requestID, blkID)
	}
	return nil
}

func (nop *BootstrapNoOps) GetFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetFailed(%s, %d) unhandled by bootstrapper. Dropped.", validatorID, requestID)
	return nil
}

func (nop *BootstrapNoOps) Gossip() error {
	nop.Ctx.Log.Debug("No Gossip during bootstrap. Dropping it")
	return nil
}

func (nop *BootstrapNoOps) Notify(Message) error {
	nop.Ctx.Log.Debug("Notify unhandled by bootstrapper. Dropped.")
	return nil
}

func (nop *BootstrapNoOps) Shutdown() error {
	nop.Ctx.Log.Debug("Called Shutdown during bootstrap. Doing nothing for now")
	return nil
}

func (nop *BootstrapNoOps) PullQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID) error {
	nop.Ctx.Log.Debug("PullQuery(%s, %d, %s) unhandled by bootstrapper. Dropped.", vdr, requestID, blkID)
	return nil
}

func (nop *BootstrapNoOps) PushQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	nop.Ctx.Log.Debug("PushQuery(%s, %d, %s) unhandled by bootstrapper. Dropped.", vdr, requestID, blkID)
	return nil
}

func (nop *BootstrapNoOps) Chits(vdr ids.ShortID, requestID uint32, votes []ids.ID) error {
	nop.Ctx.Log.Debug("Chits(%s, %d) unhandled by bootstrapper. Dropped.", vdr, requestID)
	return nil
}

func (nop *BootstrapNoOps) QueryFailed(vdr ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("QueryFailed(%s, %d) unhandled by bootstrapper. Dropped.", vdr, requestID)
	return nil
}

func (nop *BootstrapNoOps) GetStateSummaryFrontier(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetStateSummaryFrontier(%s, %d) unhandled by bootstrapper. Dropped.", validatorID, requestID)
	return nil
}

func (nop *BootstrapNoOps) StateSummaryFrontier(validatorID ids.ShortID, requestID uint32, summary []byte) error {
	nop.Ctx.Log.Debug("StateSummaryFrontier(%s, %d) unhandled by bootstrapper. Dropped.", validatorID, requestID)
	return nil
}

func (nop *BootstrapNoOps) GetStateSummaryFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetStateSummaryFrontierFailed(%s, %d) unhandled by bootstrapper. Dropped.", validatorID, requestID)
	return nil
}

func (nop *BootstrapNoOps) GetAcceptedStateSummary(validatorID ids.ShortID, requestID uint32, summaries [][]byte) error {
	nop.Ctx.Log.Debug("GetAcceptedStateSummary(%s, %d) unhandled by bootstrapper. Dropped.", validatorID, requestID)
	return nil
}

func (nop *BootstrapNoOps) AcceptedStateSummary(validatorID ids.ShortID, requestID uint32, summaries [][]byte) error {
	nop.Ctx.Log.Debug("AcceptedStateSummary(%s, %d) unhandled by bootstrapper. Dropped.", validatorID, requestID)
	return nil
}

func (nop *BootstrapNoOps) GetAcceptedStateSummaryFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Ctx.Log.Debug("GetAcceptedStateSummaryFailed(%s, %d) unhandled by bootstrapper. Dropped.", validatorID, requestID)
	return nil
}

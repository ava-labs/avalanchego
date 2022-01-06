// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	_ AcceptedFrontierHandler = &NoOpAcceptedFrontierHandler{}
	_ AcceptedHandler         = &NoOpAcceptedHandler{}
	_ AncestorsHandler        = &NoOpAncestorsHandler{}
	_ PutHandler              = &NoOpPutHandler{}
	_ QueryHandler            = &NoOpQueryHandler{}
	_ ChitsHandler            = &NoOpChitsHandler{}
	_ AppHandler              = &NoOpAppHandler{}
)

type NoOpAcceptedFrontierHandler struct {
	Log logging.Logger
}

func (nop *NoOpAcceptedFrontierHandler) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.Log.Debug("AcceptedFrontier(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop *NoOpAcceptedFrontierHandler) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Log.Debug("GetAcceptedFrontierFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

type NoOpAcceptedHandler struct {
	Log logging.Logger
}

func (nop *NoOpAcceptedHandler) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	nop.Log.Debug("Accepted(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop *NoOpAcceptedHandler) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Log.Debug("GetAcceptedFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

type NoOpAncestorsHandler struct {
	Log logging.Logger
}

func (nop *NoOpAncestorsHandler) Ancestors(validatorID ids.ShortID, requestID uint32, containers [][]byte) error {
	nop.Log.Debug("Ancestors(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

func (nop *NoOpAncestorsHandler) GetAncestorsFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Log.Debug("GetAncestorsFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

type NoOpPutHandler struct {
	Log logging.Logger
}

func (nop *NoOpPutHandler) Put(vdr ids.ShortID, requestID uint32, blkBytes []byte) error {
	if requestID == constants.GossipMsgRequestID {
		nop.Log.Verbo("Gossip Put(%s, %d) unhandled by this gear. Dropped.", vdr, requestID)
	} else {
		nop.Log.Debug("Put(%s, %d) unhandled by this gear. Dropped.", vdr, requestID)
	}
	return nil
}

func (nop *NoOpPutHandler) GetFailed(validatorID ids.ShortID, requestID uint32) error {
	nop.Log.Debug("GetFailed(%s, %d) unhandled by this gear. Dropped.", validatorID, requestID)
	return nil
}

type NoOpQueryHandler struct {
	Log logging.Logger
}

func (nop *NoOpQueryHandler) PullQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID) error {
	nop.Log.Debug("PullQuery(%s, %d, %s) unhandled by this gear. Dropped.", vdr, requestID, blkID)
	return nil
}

func (nop *NoOpQueryHandler) PushQuery(vdr ids.ShortID, requestID uint32, blkBytes []byte) error {
	nop.Log.Debug("PushQuery(%s, %d) unhandled by this gear. Dropped.", vdr, requestID)
	return nil
}

type NoOpChitsHandler struct {
	Log logging.Logger
}

func (nop *NoOpChitsHandler) Chits(vdr ids.ShortID, requestID uint32, votes []ids.ID) error {
	nop.Log.Debug("Chits(%s, %d) unhandled by this gear. Dropped.", vdr, requestID)
	return nil
}

func (nop *NoOpChitsHandler) QueryFailed(vdr ids.ShortID, requestID uint32) error {
	nop.Log.Debug("QueryFailed(%s, %d) unhandled by this gear. Dropped.", vdr, requestID)
	return nil
}

type NoOpAppHandler struct {
	Log logging.Logger
}

func (nop *NoOpAppHandler) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	nop.Log.Debug("AppRequest(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (nop *NoOpAppHandler) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	nop.Log.Debug("AppRequestFailed(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (nop *NoOpAppHandler) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	nop.Log.Debug("AppResponse(%s, %d) unhandled by this gear. Dropped.", nodeID, requestID)
	return nil
}

func (nop *NoOpAppHandler) AppGossip(nodeID ids.ShortID, msg []byte) error {
	nop.Log.Debug("AppGossip(%s) unhandled by this gear. Dropped.", nodeID)
	return nil
}

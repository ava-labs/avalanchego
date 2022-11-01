// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	_ StateSummaryFrontierHandler = (*noOpStateSummaryFrontierHandler)(nil)
	_ AcceptedStateSummaryHandler = (*noOpAcceptedStateSummaryHandler)(nil)
	_ AcceptedFrontierHandler     = (*noOpAcceptedFrontierHandler)(nil)
	_ AcceptedHandler             = (*noOpAcceptedHandler)(nil)
	_ AncestorsHandler            = (*noOpAncestorsHandler)(nil)
	_ PutHandler                  = (*noOpPutHandler)(nil)
	_ QueryHandler                = (*noOpQueryHandler)(nil)
	_ ChitsHandler                = (*noOpChitsHandler)(nil)
	_ AppHandler                  = (*noOpAppHandler)(nil)
)

type noOpStateSummaryFrontierHandler struct {
	log logging.Logger
}

func NewNoOpStateSummaryFrontierHandler(log logging.Logger) StateSummaryFrontierHandler {
	return &noOpStateSummaryFrontierHandler{log: log}
}

func (nop *noOpStateSummaryFrontierHandler) StateSummaryFrontier(_ context.Context, nodeID ids.NodeID, requestID uint32, _ []byte) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.StateSummaryFrontier),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpStateSummaryFrontierHandler) GetStateSummaryFrontierFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetStateSummaryFrontierFailed),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

type noOpAcceptedStateSummaryHandler struct {
	log logging.Logger
}

func NewNoOpAcceptedStateSummaryHandler(log logging.Logger) AcceptedStateSummaryHandler {
	return &noOpAcceptedStateSummaryHandler{log: log}
}

func (nop *noOpAcceptedStateSummaryHandler) AcceptedStateSummary(_ context.Context, nodeID ids.NodeID, requestID uint32, _ []ids.ID) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AcceptedStateSummary),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAcceptedStateSummaryHandler) GetAcceptedStateSummaryFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetAcceptedStateSummaryFailed),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

type noOpAcceptedFrontierHandler struct {
	log logging.Logger
}

func NewNoOpAcceptedFrontierHandler(log logging.Logger) AcceptedFrontierHandler {
	return &noOpAcceptedFrontierHandler{log: log}
}

func (nop *noOpAcceptedFrontierHandler) AcceptedFrontier(_ context.Context, nodeID ids.NodeID, requestID uint32, _ []ids.ID) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AcceptedFrontier),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAcceptedFrontierHandler) GetAcceptedFrontierFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetAcceptedFrontierFailed),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

type noOpAcceptedHandler struct {
	log logging.Logger
}

func NewNoOpAcceptedHandler(log logging.Logger) AcceptedHandler {
	return &noOpAcceptedHandler{log: log}
}

func (nop *noOpAcceptedHandler) Accepted(_ context.Context, nodeID ids.NodeID, requestID uint32, _ []ids.ID) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.Accepted),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAcceptedHandler) GetAcceptedFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetAcceptedFailed),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

type noOpAncestorsHandler struct {
	log logging.Logger
}

func NewNoOpAncestorsHandler(log logging.Logger) AncestorsHandler {
	return &noOpAncestorsHandler{log: log}
}

func (nop *noOpAncestorsHandler) Ancestors(_ context.Context, nodeID ids.NodeID, requestID uint32, _ [][]byte) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.Ancestors),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAncestorsHandler) GetAncestorsFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetAncestorsFailed),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

type noOpPutHandler struct {
	log logging.Logger
}

func NewNoOpPutHandler(log logging.Logger) PutHandler {
	return &noOpPutHandler{log: log}
}

func (nop *noOpPutHandler) Put(_ context.Context, nodeID ids.NodeID, requestID uint32, _ []byte) error {
	if requestID == constants.GossipMsgRequestID {
		nop.log.Verbo("dropping request",
			zap.String("reason", "unhandled by this gear"),
			zap.Stringer("messageOp", message.Put),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
	} else {
		nop.log.Debug("dropping request",
			zap.String("reason", "unhandled by this gear"),
			zap.Stringer("messageOp", message.Put),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
	}
	return nil
}

func (nop *noOpPutHandler) GetFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetFailed),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

type noOpQueryHandler struct {
	log logging.Logger
}

func NewNoOpQueryHandler(log logging.Logger) QueryHandler {
	return &noOpQueryHandler{log: log}
}

func (nop *noOpQueryHandler) PullQuery(_ context.Context, nodeID ids.NodeID, requestID uint32, _ ids.ID) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.PullQuery),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpQueryHandler) PushQuery(_ context.Context, nodeID ids.NodeID, requestID uint32, _ []byte) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.PushQuery),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

type noOpChitsHandler struct {
	log logging.Logger
}

func NewNoOpChitsHandler(log logging.Logger) ChitsHandler {
	return &noOpChitsHandler{log: log}
}

func (nop *noOpChitsHandler) Chits(_ context.Context, nodeID ids.NodeID, requestID uint32, _ []ids.ID) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.Chits),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpChitsHandler) QueryFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.QueryFailed),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

type noOpAppHandler struct {
	log logging.Logger
}

func NewNoOpAppHandler(log logging.Logger) AppHandler {
	return &noOpAppHandler{log: log}
}

func (nop *noOpAppHandler) CrossChainAppRequest(_ context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.CrossChainAppRequest),
		zap.Stringer("chainID", chainID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAppHandler) CrossChainAppRequestFailed(_ context.Context, chainID ids.ID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.CrossChainAppRequestFailed),
		zap.Stringer("chainID", chainID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAppHandler) CrossChainAppResponse(_ context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.CrossChainAppResponse),
		zap.Stringer("chainID", chainID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAppHandler) AppRequest(_ context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, _ []byte) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AppRequest),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAppHandler) AppRequestFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AppRequestFailed),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAppHandler) AppResponse(_ context.Context, nodeID ids.NodeID, requestID uint32, _ []byte) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AppResponse),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAppHandler) AppGossip(_ context.Context, nodeID ids.NodeID, _ []byte) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AppGossip),
		zap.Stringer("nodeID", nodeID),
	)
	return nil
}

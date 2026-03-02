// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
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
	_ InternalHandler             = (*noOpInternalHandler)(nil)
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
		zap.Stringer("messageOp", message.StateSummaryFrontierOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpStateSummaryFrontierHandler) GetStateSummaryFrontierFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetStateSummaryFrontierFailedOp),
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

func (nop *noOpAcceptedStateSummaryHandler) AcceptedStateSummary(_ context.Context, nodeID ids.NodeID, requestID uint32, _ set.Set[ids.ID]) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AcceptedStateSummaryOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAcceptedStateSummaryHandler) GetAcceptedStateSummaryFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetAcceptedStateSummaryFailedOp),
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

func (nop *noOpAcceptedFrontierHandler) AcceptedFrontier(_ context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AcceptedFrontierOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Stringer("containerID", containerID),
	)
	return nil
}

func (nop *noOpAcceptedFrontierHandler) GetAcceptedFrontierFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetAcceptedFrontierFailedOp),
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

func (nop *noOpAcceptedHandler) Accepted(_ context.Context, nodeID ids.NodeID, requestID uint32, _ set.Set[ids.ID]) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AcceptedOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAcceptedHandler) GetAcceptedFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetAcceptedFailedOp),
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
		zap.Stringer("messageOp", message.AncestorsOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAncestorsHandler) GetAncestorsFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetAncestorsFailedOp),
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
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.PutOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpPutHandler) GetFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GetFailedOp),
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

func (nop *noOpQueryHandler) PullQuery(_ context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID, requestedHeight uint64) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.PullQueryOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Stringer("containerID", containerID),
		zap.Uint64("requestedHeight", requestedHeight),
	)
	return nil
}

func (nop *noOpQueryHandler) PushQuery(_ context.Context, nodeID ids.NodeID, requestID uint32, _ []byte, requestedHeight uint64) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.PushQueryOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Uint64("requestedHeight", requestedHeight),
	)
	return nil
}

type noOpChitsHandler struct {
	log logging.Logger
}

func NewNoOpChitsHandler(log logging.Logger) ChitsHandler {
	return &noOpChitsHandler{log: log}
}

func (nop *noOpChitsHandler) Chits(_ context.Context, nodeID ids.NodeID, requestID uint32, preferredID, preferredIDAtHeight, acceptedID ids.ID, acceptedHeight uint64) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.ChitsOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Stringer("preferredID", preferredID),
		zap.Stringer("preferredIDAtHeight", preferredIDAtHeight),
		zap.Stringer("acceptedID", acceptedID),
		zap.Uint64("acceptedHeight", acceptedHeight),
	)
	return nil
}

func (nop *noOpChitsHandler) QueryFailed(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.QueryFailedOp),
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

func (nop *noOpAppHandler) AppRequest(_ context.Context, nodeID ids.NodeID, requestID uint32, _ time.Time, _ []byte) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AppRequestOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAppHandler) AppRequestFailed(_ context.Context, nodeID ids.NodeID, requestID uint32, appErr *AppError) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AppErrorOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Error(appErr),
	)
	return nil
}

func (nop *noOpAppHandler) AppResponse(_ context.Context, nodeID ids.NodeID, requestID uint32, _ []byte) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AppResponseOp),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (nop *noOpAppHandler) AppGossip(_ context.Context, nodeID ids.NodeID, _ []byte) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.AppGossipOp),
		zap.Stringer("nodeID", nodeID),
	)
	return nil
}

type noOpInternalHandler struct {
	log logging.Logger
}

func NewNoOpInternalHandler(log logging.Logger) InternalHandler {
	return &noOpInternalHandler{log: log}
}

func (nop *noOpInternalHandler) Connected(
	_ context.Context,
	nodeID ids.NodeID,
	nodeVersion *version.Application,
) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.ConnectedOp),
		zap.Stringer("nodeID", nodeID),
		zap.Stringer("version", nodeVersion),
	)
	return nil
}

func (nop *noOpInternalHandler) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.DisconnectedOp),
		zap.Stringer("nodeID", nodeID),
	)
	return nil
}

func (nop *noOpInternalHandler) Gossip(context.Context) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.GossipRequestOp),
	)
	return nil
}

func (nop *noOpInternalHandler) Shutdown(context.Context) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.String("messageOp", "shutdown"),
	)
	return nil
}

func (nop *noOpInternalHandler) Notify(_ context.Context, msg Message) error {
	nop.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
		zap.Stringer("messageOp", message.NotifyOp),
		zap.Stringer("message", msg),
	)
	return nil
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ p2p.Handler = (*Handler[*emptypb.Empty, *emptypb.Empty])(nil)

// Codes reach the peer, so they are explicit and never renumbered.
// [common.AppError.Is] matches on Code alone, p2p owns the negatives and zero.
var (
	ErrMalformedRequest = &common.AppError{
		Code:    1,
		Message: "malformed proto request",
	}
	ErrMarshalResponse = &common.AppError{
		Code:    2,
		Message: "failed to marshal proto response",
	}
)

// Responder is the per-RPC contract behind [Handler]:
//
//	(resp, nil) marshal resp and send it
//	(nil,  nil) send an empty response
//	(_,   err)  send err to the peer, see [Fault] for a server-side failure
//
// [p2p] answers every request, so nothing is ever dropped.
type Responder[Req, Resp proto.Message] interface {
	Respond(ctx context.Context, nodeID ids.NodeID, req Req) (Resp, *common.AppError)
}

// Fault logs a server-side failure and returns what the peer sees instead.
func Fault(log logging.Logger, nodeID ids.NodeID, err error) *common.AppError {
	log.Error("sync handler fault",
		zap.Stringer("nodeID", nodeID),
		zap.Error(err),
	)
	return p2p.ErrUnexpected
}

// Handler is a typed [p2p.Handler] for one EVM-sync RPC.
type Handler[Req, Resp proto.Message] struct {
	p2p.NoOpHandler
	log       logging.Logger
	newReq    func() Req
	responder Responder[Req, Resp]
}

func NewHandler[Req, Resp proto.Message](log logging.Logger, newReq func() Req, responder Responder[Req, Resp]) *Handler[Req, Resp] {
	return &Handler[Req, Resp]{log: log, newReq: newReq, responder: responder}
}

func (h *Handler[Req, Resp]) AppRequest(ctx context.Context, nodeID ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	req := h.newReq()
	if err := proto.Unmarshal(requestBytes, req); err != nil {
		return nil, ErrMalformedRequest
	}

	resp, appErr := h.responder.Respond(ctx, nodeID, req)
	if appErr != nil {
		return nil, appErr
	}

	respBytes, err := proto.Marshal(resp)
	if err != nil {
		h.log.Error("failed to marshal proto response",
			zap.Stringer("nodeID", nodeID),
			zap.Int("requestLen", len(requestBytes)),
			zap.Error(err),
		)
		return nil, ErrMarshalResponse
	}
	return respBytes, nil
}

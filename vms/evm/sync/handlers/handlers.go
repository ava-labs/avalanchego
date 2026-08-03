// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"errors"
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

var (
	ErrMalformedRequest = &common.AppError{
		Code:    p2p.ErrUnexpected.Code,
		Message: "malformed proto request",
	}
	ErrMarshalResponse = &common.AppError{
		Code:    p2p.ErrUnexpected.Code,
		Message: "failed to marshal proto response",
	}
)

// ProtoMessage is [proto.Message] plus comparable so the zero value
// signals "drop".
type ProtoMessage interface {
	proto.Message
	comparable
}

// Responder is the per-RPC contract behind [Handler]:
//
//	(resp, nil) deliver resp to the peer
//	(zero, nil) drop, no response is sent
//	(zero, err) a [common.AppError] reaches the peer unchanged, anything else
//	            is a server fault the peer sees as exactly [p2p.ErrUnexpected]
type Responder[Req, Resp ProtoMessage] interface {
	Respond(ctx context.Context, nodeID ids.NodeID, req Req) (Resp, error)
}

// Handler is a typed [p2p.Handler] for one EVM-sync RPC. Its own faults reuse
// [p2p.ErrUnexpected]'s code with a distinct message, so match on code.
type Handler[Req, Resp ProtoMessage] struct {
	p2p.NoOpHandler
	log       logging.Logger
	newReq    func() Req
	responder Responder[Req, Resp]
}

func NewHandler[Req, Resp ProtoMessage](log logging.Logger, newReq func() Req, responder Responder[Req, Resp]) *Handler[Req, Resp] {
	return &Handler[Req, Resp]{log: log, newReq: newReq, responder: responder}
}

func (h *Handler[Req, Resp]) AppRequest(ctx context.Context, nodeID ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	req := h.newReq()
	if err := proto.Unmarshal(requestBytes, req); err != nil {
		return nil, ErrMalformedRequest
	}

	resp, err := h.responder.Respond(ctx, nodeID, req)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			return nil, appErr
		}
		h.log.Error("sync handler server error",
			zap.Stringer("nodeID", nodeID),
			zap.Any("request", req),
			zap.Error(err),
		)
		return nil, p2p.ErrUnexpected
	}
	var zero Resp
	if resp == zero {
		return nil, nil
	}

	respBytes, err := proto.Marshal(resp)
	if err != nil {
		h.log.Error("failed to marshal proto response",
			zap.Stringer("nodeID", nodeID),
			zap.Any("request", req),
			zap.Error(err),
		)
		return nil, ErrMarshalResponse
	}
	return respBytes, nil
}

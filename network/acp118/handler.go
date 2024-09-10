// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var _ p2p.Handler = (*Handler)(nil)

// Attestor defines whether to a warp message payload should be attested to
type Attestor interface {
	Attest(message *warp.UnsignedMessage, justification []byte) (bool, error)
}

// NewHandler returns an instance of Handler
func NewHandler(
	attestor Attestor,
	signer warp.Signer,
	networkID uint32,
	chainID ids.ID,
) *Handler {
	return &Handler{
		attestor:  attestor,
		signer:    signer,
		networkID: networkID,
		chainID:   chainID,
	}
}

// Handler signs warp messages
type Handler struct {
	p2p.NoOpHandler

	attestor  Attestor
	signer    warp.Signer
	networkID uint32
	chainID   ids.ID
}

func (h *Handler) AppRequest(
	_ context.Context,
	_ ids.NodeID,
	_ time.Time,
	requestBytes []byte,
) ([]byte, *common.AppError) {
	request := &sdk.SignatureRequest{}
	if err := proto.Unmarshal(requestBytes, request); err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to unmarshal request: %s", err),
		}
	}

	msg, err := warp.ParseUnsignedMessage(request.Message)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to initialize warp unsigned message: %s", err),
		}
	}

	ok, err := h.attestor.Attest(msg, request.Justification)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to attest request: %s", err),
		}
	}

	if !ok {
		return nil, p2p.ErrAttestFailed
	}

	signature, err := h.signer.Sign(msg)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to sign message: %s", err),
		}
	}

	response := &sdk.SignatureResponse{
		Signature: signature,
	}

	responseBytes, err := proto.Marshal(response)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to marshal response: %s", err),
		}
	}

	return responseBytes, nil
}

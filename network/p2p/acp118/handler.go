// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const HandlerID = p2p.SignatureRequestHandlerID

var _ p2p.Handler = (*Handler)(nil)

// Verifier verifies that a warp message should be signed
type Verifier interface {
	Verify(
		ctx context.Context,
		message *warp.UnsignedMessage,
		justification []byte,
	) *common.AppError
}

// NewHandler returns an instance of Handler
func NewHandler(verifier Verifier, signer warp.Signer) *Handler {
	return NewCachedHandler(
		&cache.Empty[ids.ID, []byte]{},
		verifier,
		signer,
	)
}

// NewCachedHandler returns an instance of Handler that caches successful
// signatures.
func NewCachedHandler(
	cacher cache.Cacher[ids.ID, []byte],
	verifier Verifier,
	signer warp.Signer,
) *Handler {
	return &Handler{
		signatureCache: cacher,
		verifier:       verifier,
		signer:         signer,
	}
}

// Handler signs warp messages
type Handler struct {
	p2p.NoOpHandler

	signatureCache cache.Cacher[ids.ID, []byte]
	verifier       Verifier
	signer         warp.Signer
}

func (h *Handler) AppRequest(
	ctx context.Context,
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
			Message: fmt.Sprintf("failed to parse warp unsigned message: %s", err),
		}
	}

	msgID := msg.ID()
	if signatureBytes, ok := h.signatureCache.Get(msgID); ok {
		return signatureToResponse(signatureBytes)
	}

	if err := h.verifier.Verify(ctx, msg, request.Justification); err != nil {
		return nil, err
	}

	signature, err := h.signer.Sign(msg)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to sign message: %s", err),
		}
	}

	h.signatureCache.Put(msgID, signature)
	return signatureToResponse(signature)
}

func signatureToResponse(signature []byte) ([]byte, *common.AppError) {
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

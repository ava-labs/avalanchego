// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/warp"
	"github.com/ava-labs/subnet-evm/warp/handlers/stats"
	"github.com/ethereum/go-ethereum/log"
)

// SignatureRequestHandler is a peer.RequestHandler for message.SignatureRequest
// serving requested BLS signature data
type SignatureRequestHandler interface {
	OnSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, signatureRequest message.SignatureRequest) ([]byte, error)
}

// signatureRequestHandler implements the SignatureRequestHandler interface
type signatureRequestHandler struct {
	backend warp.WarpBackend
	codec   codec.Manager
	stats   stats.SignatureRequestHandlerStats
}

func NewSignatureRequestHandler(backend warp.WarpBackend, codec codec.Manager, stats stats.SignatureRequestHandlerStats) SignatureRequestHandler {
	return &signatureRequestHandler{
		backend: backend,
		codec:   codec,
		stats:   stats,
	}
}

// OnSignatureRequest handles message.SignatureRequest, and retrieves a warp signature for the requested message ID.
// Never returns an error
// Expects returned errors to be treated as FATAL
// Returns empty response if signature is not found
// Assumes ctx is active
func (s *signatureRequestHandler) OnSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, signatureRequest message.SignatureRequest) ([]byte, error) {
	startTime := time.Now()
	s.stats.IncSignatureRequest()

	// Always report signature request time
	defer func() {
		s.stats.UpdateSignatureRequestTime(time.Since(startTime))
	}()

	signature, err := s.backend.GetSignature(signatureRequest.MessageID)
	if err != nil {
		log.Debug("Unknown warp signature requested", "messageID", signatureRequest.MessageID)
		s.stats.IncSignatureMiss()
		return nil, nil
	}

	s.stats.IncSignatureHit()
	response := message.SignatureResponse{Signature: signature}
	responseBytes, err := s.codec.Marshal(message.Version, &response)
	if err != nil {
		log.Error("could not marshal SignatureResponse, dropping request", "nodeID", nodeID, "requestID", requestID, "err", err)
		return nil, nil
	}

	return responseBytes, nil
}

type NoopSignatureRequestHandler struct{}

func (s *NoopSignatureRequestHandler) OnSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, signatureRequest message.SignatureRequest) ([]byte, error) {
	return nil, nil
}

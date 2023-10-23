// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/warp"
	"github.com/ethereum/go-ethereum/log"
)

// SignatureRequestHandler serves warp signature requests. It is a peer.RequestHandler for message.MessageSignatureRequest.
type SignatureRequestHandler struct {
	backend warp.Backend
	codec   codec.Manager
	stats   *handlerStats
}

func NewSignatureRequestHandler(backend warp.Backend, codec codec.Manager) *SignatureRequestHandler {
	return &SignatureRequestHandler{
		backend: backend,
		codec:   codec,
		stats:   newStats(),
	}
}

// OnMessageSignatureRequest handles message.MessageSignatureRequest, and retrieves a warp signature for the requested message ID.
// Never returns an error
// Expects returned errors to be treated as FATAL
// Returns empty response if signature is not found
// Assumes ctx is active
func (s *SignatureRequestHandler) OnMessageSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, signatureRequest message.MessageSignatureRequest) ([]byte, error) {
	startTime := time.Now()
	s.stats.IncMessageSignatureRequest()

	// Always report signature request time
	defer func() {
		s.stats.UpdateMessageSignatureRequestTime(time.Since(startTime))
	}()

	signature, err := s.backend.GetMessageSignature(signatureRequest.MessageID)
	if err != nil {
		log.Debug("Unknown warp signature requested", "messageID", signatureRequest.MessageID)
		s.stats.IncMessageSignatureMiss()
		signature = [bls.SignatureLen]byte{}
	} else {
		s.stats.IncMessageSignatureHit()
	}

	response := message.SignatureResponse{Signature: signature}
	responseBytes, err := s.codec.Marshal(message.Version, &response)
	if err != nil {
		log.Error("could not marshal SignatureResponse, dropping request", "nodeID", nodeID, "requestID", requestID, "err", err)
		return nil, nil
	}

	return responseBytes, nil
}

func (s *SignatureRequestHandler) OnBlockSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request message.BlockSignatureRequest) ([]byte, error) {
	startTime := time.Now()
	s.stats.IncBlockSignatureRequest()

	// Always report signature request time
	defer func() {
		s.stats.UpdateBlockSignatureRequestTime(time.Since(startTime))
	}()

	signature, err := s.backend.GetBlockSignature(request.BlockID)
	if err != nil {
		log.Debug("Unknown warp signature requested", "blockID", request.BlockID)
		s.stats.IncBlockSignatureMiss()
		signature = [bls.SignatureLen]byte{}
	} else {
		s.stats.IncBlockSignatureHit()
	}

	response := message.SignatureResponse{Signature: signature}
	responseBytes, err := s.codec.Marshal(message.Version, &response)
	if err != nil {
		log.Error("could not marshal SignatureResponse, dropping request", "nodeID", nodeID, "requestID", requestID, "err", err)
		return nil, nil
	}

	return responseBytes, nil
}

type NoopSignatureRequestHandler struct{}

func (s *NoopSignatureRequestHandler) OnMessageSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, signatureRequest message.MessageSignatureRequest) ([]byte, error) {
	return nil, nil
}

func (s *NoopSignatureRequestHandler) OnBlockSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, signatureRequest message.BlockSignatureRequest) ([]byte, error) {
	return nil, nil
}

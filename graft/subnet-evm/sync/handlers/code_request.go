// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/sync/handlers/stats"
	"github.com/ava-labs/avalanchego/ids"
)

// CodeRequestHandler is a peer.RequestHandler for message.CodeRequest
// serving requested contract code bytes
type CodeRequestHandler struct {
	codeReader ethdb.KeyValueReader
	codec      codec.Manager
	stats      stats.CodeRequestHandlerStats
}

func NewCodeRequestHandler(codeReader ethdb.KeyValueReader, codec codec.Manager, stats stats.CodeRequestHandlerStats) *CodeRequestHandler {
	handler := &CodeRequestHandler{
		codeReader: codeReader,
		codec:      codec,
		stats:      stats,
	}
	return handler
}

// OnCodeRequest handles request to retrieve contract code by its hash in message.CodeRequest
// Never returns error
// Returns nothing if code hash is not found
// Expects returned errors to be treated as FATAL
// Assumes ctx is active
func (n *CodeRequestHandler) OnCodeRequest(_ context.Context, nodeID ids.NodeID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
	startTime := time.Now()
	n.stats.IncCodeRequest()

	// always report code read time metric
	defer func() {
		n.stats.UpdateCodeReadTime(time.Since(startTime))
	}()

	if len(codeRequest.Hashes) > message.MaxCodeHashesPerRequest {
		n.stats.IncTooManyHashesRequested()
		log.Debug("too many hashes requested, dropping request", "nodeID", nodeID, "requestID", requestID, "numHashes", len(codeRequest.Hashes))
		return nil, nil
	}
	if !isUnique(codeRequest.Hashes) {
		n.stats.IncDuplicateHashesRequested()
		log.Debug("duplicate code hashes requested, dropping request", "nodeID", nodeID, "requestID", requestID)
		return nil, nil
	}

	codeBytes := make([][]byte, len(codeRequest.Hashes))
	totalBytes := 0
	for i, hash := range codeRequest.Hashes {
		codeBytes[i] = rawdb.ReadCode(n.codeReader, hash)
		if len(codeBytes[i]) == 0 {
			n.stats.IncMissingCodeHash()
			log.Debug("requested code not found, dropping request", "nodeID", nodeID, "requestID", requestID, "hash", hash)
			return nil, nil
		}
		totalBytes += len(codeBytes[i])
	}

	codeResponse := message.CodeResponse{Data: codeBytes}
	responseBytes, err := n.codec.Marshal(message.Version, codeResponse)
	if err != nil {
		log.Error("could not marshal CodeResponse, dropping request", "nodeID", nodeID, "requestID", requestID, "request", codeRequest, "err", err)
		return nil, nil
	}
	n.stats.UpdateCodeBytesReturned(uint32(totalBytes))
	return responseBytes, nil
}

func isUnique(hashes []common.Hash) bool {
	seen := make(map[common.Hash]struct{})
	for _, hash := range hashes {
		if _, found := seen[hash]; found {
			return false
		}
		seen[hash] = struct{}{}
	}
	return true
}

// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/handlers/stats"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	// parentLimit specifies how many parents to retrieve and send given a starting hash
	// This value overrides any specified limit in blockRequest.Parents if it is greater than this value
	parentLimit           = uint16(64)
	targetMessageByteSize = units.MiB - units.KiB // Target total block bytes slightly under original network codec max size of 1MB
)

// BlockRequestHandler is a peer.RequestHandler for message.BlockRequest
// serving requested blocks starting at specified hash
type BlockRequestHandler struct {
	stats         stats.BlockRequestHandlerStats
	blockProvider BlockProvider
	codec         codec.Manager
}

func NewBlockRequestHandler(blockProvider BlockProvider, codec codec.Manager, handlerStats stats.BlockRequestHandlerStats) *BlockRequestHandler {
	return &BlockRequestHandler{
		blockProvider: blockProvider,
		codec:         codec,
		stats:         handlerStats,
	}
}

// OnBlockRequest handles incoming message.BlockRequest, returning blocks as requested
// Never returns error
// Expects returned errors to be treated as FATAL
// Returns empty response or subset of requested blocks if ctx expires during fetch
// Assumes ctx is active
func (b *BlockRequestHandler) OnBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error) {
	startTime := time.Now()
	b.stats.IncBlockRequest()

	// override given Parents limit if it is greater than parentLimit
	parents := blockRequest.Parents
	if parents > parentLimit {
		parents = parentLimit
	}
	blocks := make([][]byte, 0, parents)
	totalBytes := 0

	// ensure metrics are captured properly on all return paths
	defer func() {
		b.stats.UpdateBlockRequestProcessingTime(time.Since(startTime))
		b.stats.UpdateBlocksReturned(uint16(len(blocks)))
	}()

	hash := blockRequest.Hash
	height := blockRequest.Height
	for i := 0; i < int(parents); i++ {
		// we return whatever we have until ctx errors, limit is exceeded, or we reach the genesis block
		// this will happen either when the ctx is cancelled or we hit the ctx deadline
		if ctx.Err() != nil {
			break
		}

		if (hash == common.Hash{}) {
			break
		}

		block := b.blockProvider.GetBlock(hash, height)
		if block == nil {
			b.stats.IncMissingBlockHash()
			break
		}

		buf := new(bytes.Buffer)
		if err := block.EncodeRLP(buf); err != nil {
			log.Error("failed to RLP encode block", "hash", block.Hash(), "height", block.NumberU64(), "err", err)
			return nil, nil
		}

		if buf.Len()+totalBytes > targetMessageByteSize && len(blocks) > 0 {
			log.Debug("Skipping block due to max total bytes size", "totalBlockDataSize", totalBytes, "blockSize", buf.Len(), "maxTotalBytesSize", targetMessageByteSize)
			break
		}

		blocks = append(blocks, buf.Bytes())
		totalBytes += buf.Len()
		hash = block.ParentHash()
		height--
	}

	if len(blocks) == 0 {
		// drop this request
		log.Debug("no requested blocks found, dropping request", "nodeID", nodeID, "requestID", requestID, "hash", blockRequest.Hash, "parents", blockRequest.Parents)
		return nil, nil
	}

	response := message.BlockResponse{
		Blocks: blocks,
	}
	responseBytes, err := b.codec.Marshal(message.Version, response)
	if err != nil {
		log.Error("failed to marshal BlockResponse, dropping request", "nodeID", nodeID, "requestID", requestID, "hash", blockRequest.Hash, "parents", blockRequest.Parents, "blocksLen", len(response.Blocks), "err", err)
		return nil, nil
	}

	return responseBytes, nil
}

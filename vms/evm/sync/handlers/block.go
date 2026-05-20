// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"bytes"
	"context"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	// MaxParentsPerRequest caps the parent count per response.
	MaxParentsPerRequest = uint16(64)

	// targetBlockResponseBytes is the soft cap on total RLP bytes per
	// response. Sized just under 1 MiB for the legacy codec's
	// per-message limit.
	targetBlockResponseBytes = units.MiB - units.KiB
)

// BlockHandler serves [syncpb.GetBlockRequest] over [p2p.EVMBlockRequestHandlerID].
type BlockHandler = Handler[*syncpb.GetBlockRequest, *syncpb.GetBlockResponse]

// BlockResponder is the inner contract for block-batch requests.
type BlockResponder = Responder[*syncpb.GetBlockRequest, *syncpb.GetBlockResponse]

// NewBlockHandler wires resp into a [BlockHandler].
func NewBlockHandler(resp BlockResponder) *BlockHandler {
	return NewHandler(func() *syncpb.GetBlockRequest { return &syncpb.GetBlockRequest{} }, resp)
}

// BlockProvider returns blocks by hash/height and by canonical height.
// nil stops the parent walk.
type BlockProvider interface {
	GetBlock(hash common.Hash, height uint64) *types.Block
	// GetBlockByHeight returns the canonical block at the given height,
	// or nil if none is known.
	GetBlockByHeight(height uint64) *types.Block
}

var _ BlockResponder = (*blockResponder)(nil)

// blockResponder walks the parent chain from the canonical block at the
// requested height via a [BlockProvider].
type blockResponder struct {
	blocks BlockProvider
	stats  BlockStats
}

func NewBlockResponder(blocks BlockProvider, stats BlockStats) BlockResponder {
	return &blockResponder{blocks: blocks, stats: stats}
}

func (r *blockResponder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetBlockRequest) (*syncpb.GetBlockResponse, error) {
	startTime := time.Now()
	r.stats.IncBlockRequest()

	parents := uint16(min(req.GetParents(), uint32(MaxParentsPerRequest)))

	encoded := make([][]byte, 0, parents)
	totalBytes := 0
	defer func() {
		r.stats.UpdateBlockRequestProcessingTime(time.Since(startTime))
		r.stats.UpdateBlocksReturned(uint16(len(encoded)))
	}()

	block := r.blocks.GetBlockByHeight(req.GetHeight())
	for range parents {
		if ctx.Err() != nil {
			break
		}
		if block == nil {
			r.stats.IncMissingBlockHash()
			break
		}

		buf := new(bytes.Buffer)
		if err := block.EncodeRLP(buf); err != nil {
			log.Error("failed to RLP encode block", "hash", block.Hash(), "height", block.NumberU64(), "err", err)
			return nil, nil
		}
		if buf.Len()+totalBytes > targetBlockResponseBytes && len(encoded) > 0 {
			log.Debug("skipping block due to max total bytes size", "totalBlockDataSize", totalBytes, "blockSize", buf.Len(), "max", targetBlockResponseBytes)
			break
		}

		encoded = append(encoded, buf.Bytes())
		totalBytes += buf.Len()
		if block.NumberU64() == 0 {
			break
		}
		block = r.blocks.GetBlock(block.ParentHash(), block.NumberU64()-1)
	}

	if len(encoded) == 0 {
		log.Debug("no requested blocks found, dropping request", "nodeID", nodeID, "height", req.GetHeight(), "parents", req.GetParents())
		return nil, nil
	}
	return &syncpb.GetBlockResponse{Blocks: encoded}, nil
}

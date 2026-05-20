// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"bytes"
	"context"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	// MaxParentsPerRequest caps the parent count per response.
	MaxParentsPerRequest = uint16(64)

	// targetResponseBytes is the soft cap on total RLP bytes per
	// response, set just under 1 MiB.
	targetResponseBytes = units.MiB - units.KiB
)

type (
	// Handler serves [syncpb.GetBlockRequest] over [p2p.EVMBlockRequestHandlerID].
	Handler = handlers.Handler[*syncpb.GetBlockRequest, *syncpb.GetBlockResponse]
	// Responder serves block-batch requests.
	Responder = handlers.Responder[*syncpb.GetBlockRequest, *syncpb.GetBlockResponse]
)

// NewHandler wires resp into a [Handler].
func NewHandler(log logging.Logger, resp Responder) *Handler {
	return handlers.NewHandler(log, func() *syncpb.GetBlockRequest { return &syncpb.GetBlockRequest{} }, resp)
}

// Provider returns blocks by (hash, height) or by canonical height.
// A nil return stops the parent walk.
type Provider interface {
	GetBlock(hash common.Hash, height uint64) *types.Block
	GetBlockByHeight(height uint64) *types.Block
}

var _ Responder = (*responder)(nil)

// responder walks the parent chain from the canonical block at the
// requested height.
type responder struct {
	blocks Provider
}

func NewResponder(blocks Provider) Responder {
	return &responder{blocks: blocks}
}

func (r *responder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetBlockRequest) (*syncpb.GetBlockResponse, error) {
	parents := uint16(min(req.GetNumParents(), uint32(MaxParentsPerRequest)))

	encoded := make([][]byte, 0, parents)
	totalBytes := 0

	block := r.blocks.GetBlockByHeight(req.GetHeight())
	for range parents {
		if ctx.Err() != nil {
			break
		}
		if block == nil {
			log.Debug("requested block not found, stopping parent walk", "nodeID", nodeID)
			break
		}

		buf := new(bytes.Buffer)
		if err := block.EncodeRLP(buf); err != nil {
			log.Error("failed to RLP encode block", "hash", block.Hash(), "height", block.NumberU64(), "err", err)
			return nil, nil
		}
		if buf.Len()+totalBytes > targetResponseBytes && len(encoded) > 0 {
			log.Debug("skipping block due to max total bytes size", "totalBlockDataSize", totalBytes, "blockSize", buf.Len(), "max", targetResponseBytes)
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
		log.Debug("no requested blocks found, dropping request", "nodeID", nodeID, "height", req.GetHeight(), "parents", req.GetNumParents())
		return nil, nil
	}
	return &syncpb.GetBlockResponse{Blocks: encoded}, nil
}

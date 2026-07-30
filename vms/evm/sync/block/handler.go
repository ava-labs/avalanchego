// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"bytes"
	"context"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	// maxParentsPerRequest bounds the parent walk per request. A block has no
	// fixed size, so targetResponseBytes bounds the response, and this only
	// caps how many blocks the handler looks up and encodes for one request.
	maxParentsPerRequest = uint16(64)

	// targetResponseBytes caps the total block bytes per response at the usable
	// p2p message budget.
	targetResponseBytes = constants.MaxContainersLen
)

// RegisterHandler serves block-batch requests at [p2p.EVMBlockRequestHandlerID] on net.
func RegisterHandler(log logging.Logger, net *p2p.Network, blocks Provider) error {
	h := handlers.NewHandler(
		log,
		func() *syncpb.GetBlockRequest { return &syncpb.GetBlockRequest{} },
		newResponder(log, blocks),
	)
	return net.AddHandler(p2p.EVMBlockRequestHandlerID, h)
}

// Provider returns blocks by (hash, height) or by canonical height.
// A nil return stops the parent walk.
type Provider interface {
	GetBlock(hash common.Hash, height uint64) *types.Block
	GetBlockByHeight(height uint64) *types.Block
}

var _ handlers.Responder[*syncpb.GetBlockRequest, *syncpb.GetBlockResponse] = (*responder)(nil)

// responder walks the parent chain from the canonical block at the
// requested height.
type responder struct {
	log    logging.Logger
	blocks Provider
}

func newResponder(log logging.Logger, blocks Provider) *responder {
	return &responder{log: log, blocks: blocks}
}

func (r *responder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetBlockRequest) (*syncpb.GetBlockResponse, error) {
	parents := uint16(min(req.GetNumParents(), uint32(maxParentsPerRequest)))

	encoded := make([][]byte, 0, parents)
	totalBytes := 0

	block := r.blocks.GetBlockByHeight(req.GetHeight())
	for range parents {
		if ctx.Err() != nil {
			break
		}
		if block == nil {
			r.log.Debug("requested block not found, stopping parent walk",
				zap.Stringer("nodeID", nodeID),
			)
			break
		}

		buf := new(bytes.Buffer)
		if err := block.EncodeRLP(buf); err != nil {
			r.log.Error("failed to RLP encode block",
				zap.Stringer("hash", block.Hash()),
				zap.Uint64("height", block.NumberU64()),
				zap.Error(err),
			)
			return nil, nil
		}
		if buf.Len()+totalBytes > targetResponseBytes && len(encoded) > 0 {
			r.log.Debug("skipping block due to max total bytes size",
				zap.Int("totalBlockDataSize", totalBytes),
				zap.Int("blockSize", buf.Len()),
				zap.Int("max", targetResponseBytes),
			)
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
		r.log.Debug("no requested blocks found, dropping request",
			zap.Stringer("nodeID", nodeID),
			zap.Uint64("height", req.GetHeight()),
			zap.Uint32("parents", req.GetNumParents()),
		)
		return nil, nil
	}
	return &syncpb.GetBlockResponse{Blocks: encoded}, nil
}

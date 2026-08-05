// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

const (
	// maxParentsPerRequest caps the parent walk. Blocks vary in size, so
	// maxResponseBytes bounds the response itself.
	maxParentsPerRequest = uint16(64)

	// defaultMaxResponseBytes is the conservative p2p budget. A chain with
	// larger blocks must raise it through [WithMaxResponseBytes].
	defaultMaxResponseBytes = constants.MaxContainersLen
)

var (
	errBlocksNotFound = &avacommon.AppError{
		Code:    2000,
		Message: "requested blocks not found",
	}
	errNoParentsRequested = &avacommon.AppError{
		Code:    2001,
		Message: "no parents requested",
	}
	errServingCancelled = &avacommon.AppError{
		Code:    2002,
		Message: "serving cancelled",
	}
)

type handlerConfig struct {
	maxResponseBytes int
}

// HandlerOption configures [RegisterHandler].
type HandlerOption = options.Option[handlerConfig]

// WithMaxResponseBytes caps response bytes, ideally the chain's max block size.
func WithMaxResponseBytes(n int) HandlerOption {
	return options.Func[handlerConfig](func(c *handlerConfig) {
		if n > 0 {
			c.maxResponseBytes = n
		}
	})
}

// RegisterHandler serves block-batch requests at [p2p.EVMBlockRequestHandlerID] on net.
func RegisterHandler(log logging.Logger, net *p2p.Network, blocks Provider, opts ...HandlerOption) error {
	h := handlers.NewHandler[syncpb.GetBlockRequest](log, newResponder(log, blocks, opts...))
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
	log              logging.Logger
	blocks           Provider
	maxResponseBytes int
}

func newResponder(log logging.Logger, blocks Provider, opts ...HandlerOption) *responder {
	cfg := handlerConfig{maxResponseBytes: defaultMaxResponseBytes}
	options.ApplyTo(&cfg, opts...)

	return &responder{log: log, blocks: blocks, maxResponseBytes: cfg.maxResponseBytes}
}

func (r *responder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetBlockRequest) (*syncpb.GetBlockResponse, *avacommon.AppError) {
	parents := uint16(min(req.GetNumParents(), uint32(maxParentsPerRequest)))
	if parents == 0 {
		r.log.Debug("rejecting request, no parents requested",
			zap.Stringer("nodeID", nodeID),
		)
		return nil, errNoParentsRequested
	}

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
			return nil, handlers.Fault(r.log, nodeID,
				fmt.Errorf("encoding block %s at height %d: %w", block.Hash(), block.NumberU64(), err))
		}
		// Serve an oversized block alone rather than stall.
		if buf.Len()+totalBytes > r.maxResponseBytes && len(encoded) > 0 {
			r.log.Debug("skipping block due to max total bytes size",
				zap.Int("totalBlockDataSize", totalBytes),
				zap.Int("blockSize", buf.Len()),
				zap.Int("max", r.maxResponseBytes),
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
		// Tell the peer we gave up rather than that the blocks are missing.
		if ctx.Err() != nil {
			return nil, errServingCancelled
		}
		r.log.Debug("rejecting request, no blocks found",
			zap.Stringer("nodeID", nodeID),
			zap.Uint64("height", req.GetHeight()),
			zap.Uint32("parents", req.GetNumParents()),
		)
		return nil, errBlocksNotFound
	}
	return &syncpb.GetBlockResponse{Blocks: encoded}, nil
}

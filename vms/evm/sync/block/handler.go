// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"time"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
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

// RegisterHandler serves block-batch requests at [p2p.EVMBlockRequestHandlerID] on net.
func RegisterHandler(log logging.Logger, net *p2p.Network, db ethdb.Reader, opts ...HandlerOption) error {
	h := handlers.NewHandler(log, newResponder(log, db, opts...))
	return net.AddHandler(p2p.EVMBlockRequestHandlerID, h)
}

type handlerConfig struct {
	maxResponseBytes int
}

// HandlerOption configures [RegisterHandler].
type HandlerOption = options.Option[handlerConfig]

// WithMaxResponseBytes caps response bytes, at minimum the chain's max block
// size. The default is below C-Chain's, so a single block can exceed it.
func WithMaxResponseBytes(n int) HandlerOption {
	return options.Func[handlerConfig](func(c *handlerConfig) {
		if n > 0 {
			c.maxResponseBytes = n
		}
	})
}

var _ handlers.Responder[*syncpb.GetBlockRequest, *syncpb.GetBlockResponse] = (*responder)(nil)

// responder serves the requested block and its accepted ancestors.
type responder struct {
	log              logging.Logger
	db               ethdb.Reader
	maxResponseBytes int
}

func newResponder(log logging.Logger, db ethdb.Reader, opts ...HandlerOption) *responder {
	cfg := handlerConfig{maxResponseBytes: defaultMaxResponseBytes}
	options.ApplyTo(&cfg, opts...)

	return &responder{log: log, db: db, maxResponseBytes: cfg.maxResponseBytes}
}

const (
	// maxParentsPerRequest caps the parent walk. Blocks vary in size, so
	// maxResponseBytes bounds the response itself.
	maxParentsPerRequest = uint16(64)

	// maxBlocksPerResponse counts the requested block alongside its parents.
	maxBlocksPerResponse = int(maxParentsPerRequest) + 1

	// defaultMaxResponseBytes is the conservative p2p budget. A chain with
	// larger blocks must raise it through [WithMaxResponseBytes].
	defaultMaxResponseBytes = constants.MaxContainersLen

	// maxBlocksRetrievalTime matches the node's default for the equivalent
	// bootstrap GetAncestors operation.
	maxBlocksRetrievalTime = 50 * time.Millisecond
)

var (
	errBlocksNotFound = &avacommon.AppError{
		Code:    2000,
		Message: "requested blocks not found",
	}
	errServingCancelled = &avacommon.AppError{
		Code:    2001,
		Message: "serving cancelled",
	}
)

func (r *responder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetBlockRequest) (*syncpb.GetBlockResponse, *avacommon.AppError) {
	// The request counts parents, so the response carries one more block.
	blocks := int(min(req.GetNumParents(), uint32(maxParentsPerRequest))) + 1

	// [GetAncestors] re-derives the height from the hash, so this lookup is
	// redundant with its own. An unknown height yields no blocks below.
	hash := rawdb.ReadCanonicalHash(r.db, req.GetHeight())
	served, err := GetAncestors(ctx, r.db, ids.ID(hash), blocks, r.maxResponseBytes, maxBlocksRetrievalTime)
	if err != nil {
		return nil, handlers.Fault(r.log, nodeID, err)
	}
	if len(served) == 0 {
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
	return &syncpb.GetBlockResponse{Blocks: served}, nil
}

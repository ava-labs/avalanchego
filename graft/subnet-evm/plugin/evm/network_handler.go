// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"

	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats"
	"github.com/ava-labs/avalanchego/ids"
)

var _ message.RequestHandler = (*networkHandler)(nil)

type networkHandler struct {
	leafRequestHandler  handlers.LeafRequestHandler
	blockRequestHandler *handlers.BlockRequestHandler
	codeRequestHandler  *handlers.CodeRequestHandler
}

// newNetworkHandler constructs the handler for serving network requests.
// leafRequestHandler MAY be nil, in which case leaf requests are dropped.
func newNetworkHandler(
	provider handlers.SyncDataProvider,
	diskDB ethdb.KeyValueReader,
	networkCodec codec.Manager,
	leafRequestHandler handlers.LeafRequestHandler,
	syncStats stats.HandlerStats,
) *networkHandler {
	return &networkHandler{
		leafRequestHandler:  leafRequestHandler,
		blockRequestHandler: handlers.NewBlockRequestHandler(provider, networkCodec, syncStats),
		codeRequestHandler:  handlers.NewCodeRequestHandler(diskDB, networkCodec, syncStats),
	}
}

func (n networkHandler) HandleLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	if n.leafRequestHandler == nil {
		log.Debug("leaf requests are not served, dropping request", "nodeID", nodeID, "requestID", requestID)
		return nil, nil
	}
	return n.leafRequestHandler.OnLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (n networkHandler) HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error) {
	return n.blockRequestHandler.OnBlockRequest(ctx, nodeID, requestID, blockRequest)
}

func (n networkHandler) HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
	return n.codeRequestHandler.OnCodeRequest(ctx, nodeID, requestID, codeRequest)
}

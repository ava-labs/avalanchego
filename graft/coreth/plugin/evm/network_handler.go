// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"

	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/handlers/stats"
	"github.com/ava-labs/avalanchego/ids"

	syncHandlers "github.com/ava-labs/avalanchego/graft/coreth/sync/handlers"
)

var _ message.RequestHandler = (*networkHandler)(nil)

type LeafHandlers map[message.NodeType]syncHandlers.LeafRequestHandler

type networkHandler struct {
	leafRequestHandlers LeafHandlers
	blockRequestHandler *syncHandlers.BlockRequestHandler
	codeRequestHandler  *syncHandlers.CodeRequestHandler
}

type LeafRequestTypeConfig struct {
	NodeType     message.NodeType
	NodeKeyLen   int
	TrieDB       *triedb.Database
	UseSnapshots bool
	MetricName   string
}

// newNetworkHandler constructs the handler for serving network requests.
func newNetworkHandler(
	provider syncHandlers.SyncDataProvider,
	diskDB ethdb.KeyValueReader,
	networkCodec codec.Manager,
	leafRequestHandlers LeafHandlers,
	syncStats stats.HandlerStats,
) *networkHandler {
	return &networkHandler{
		leafRequestHandlers: leafRequestHandlers,
		blockRequestHandler: syncHandlers.NewBlockRequestHandler(provider, networkCodec, syncStats),
		codeRequestHandler:  syncHandlers.NewCodeRequestHandler(diskDB, networkCodec, syncStats),
	}
}

func (n networkHandler) HandleLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	handler, ok := n.leafRequestHandlers[leafsRequest.NodeType]
	if !ok {
		log.Debug("node type is not recognised, dropping request", "nodeID", nodeID, "requestID", requestID, "nodeType", leafsRequest.NodeType)
		return nil, nil
	}
	return handler.OnLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (n networkHandler) HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error) {
	return n.blockRequestHandler.OnBlockRequest(ctx, nodeID, requestID, blockRequest)
}

func (n networkHandler) HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
	return n.codeRequestHandler.OnCodeRequest(ctx, nodeID, requestID, codeRequest)
}

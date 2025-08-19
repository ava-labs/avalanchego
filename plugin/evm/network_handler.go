// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/metrics"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/warp"

	syncHandlers "github.com/ava-labs/subnet-evm/sync/handlers"
	syncStats "github.com/ava-labs/subnet-evm/sync/handlers/stats"
)

var _ message.RequestHandler = (*networkHandler)(nil)

type networkHandler struct {
	leafRequestHandler  *syncHandlers.LeafsRequestHandler
	blockRequestHandler *syncHandlers.BlockRequestHandler
	codeRequestHandler  *syncHandlers.CodeRequestHandler
}

// newNetworkHandler constructs the handler for serving network requests.
func newNetworkHandler(
	provider syncHandlers.SyncDataProvider,
	diskDB ethdb.KeyValueReader,
	evmTrieDB *triedb.Database,
	warpBackend warp.Backend,
	networkCodec codec.Manager,
) message.RequestHandler {
	syncStats := syncStats.NewHandlerStats(metrics.Enabled)
	return &networkHandler{
		leafRequestHandler:  syncHandlers.NewLeafsRequestHandler(evmTrieDB, nil, networkCodec, syncStats),
		blockRequestHandler: syncHandlers.NewBlockRequestHandler(provider, networkCodec, syncStats),
		codeRequestHandler:  syncHandlers.NewCodeRequestHandler(diskDB, networkCodec, syncStats),
	}
}

func (n networkHandler) HandleStateTrieLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	return n.leafRequestHandler.OnLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (n networkHandler) HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error) {
	return n.blockRequestHandler.OnBlockRequest(ctx, nodeID, requestID, blockRequest)
}

func (n networkHandler) HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
	return n.codeRequestHandler.OnCodeRequest(ctx, nodeID, requestID, codeRequest)
}

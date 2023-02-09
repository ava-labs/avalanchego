// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/handlers/stats"
	warpHandlers "github.com/ava-labs/subnet-evm/handlers/warp"
	"github.com/ava-labs/subnet-evm/metrics"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/sync/handlers"
	syncHandlers "github.com/ava-labs/subnet-evm/sync/handlers"
	"github.com/ava-labs/subnet-evm/trie"
)

var _ message.RequestHandler = &networkHandler{}

type networkHandler struct {
	stateTrieLeafsRequestHandler *syncHandlers.LeafsRequestHandler
	blockRequestHandler          *syncHandlers.BlockRequestHandler
	codeRequestHandler           *syncHandlers.CodeRequestHandler
	signatureRequestHandler      warpHandlers.SignatureRequestHandler
}

// NewNetworkHandler constructs the handler for serving network requests.
func NewNetworkHandler(
	provider handlers.SyncDataProvider,
	evmTrieDB *trie.Database,
	networkCodec codec.Manager,
) message.RequestHandler {
	handlerStats := stats.NewHandlerStats(metrics.Enabled)
	return &networkHandler{
		// State sync handlers
		stateTrieLeafsRequestHandler: syncHandlers.NewLeafsRequestHandler(evmTrieDB, provider, networkCodec, handlerStats),
		blockRequestHandler:          syncHandlers.NewBlockRequestHandler(provider, networkCodec, handlerStats),
		codeRequestHandler:           syncHandlers.NewCodeRequestHandler(evmTrieDB.DiskDB(), networkCodec, handlerStats),

		// TODO: initialize actual signature request handler when warp is ready
		signatureRequestHandler: &warpHandlers.NoopSignatureRequestHandler{},
	}
}

func (n networkHandler) HandleTrieLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	return n.stateTrieLeafsRequestHandler.OnLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (n networkHandler) HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error) {
	return n.blockRequestHandler.OnBlockRequest(ctx, nodeID, requestID, blockRequest)
}

func (n networkHandler) HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
	return n.codeRequestHandler.OnCodeRequest(ctx, nodeID, requestID, codeRequest)
}

func (n networkHandler) HandleSignatureRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, signatureRequest message.SignatureRequest) ([]byte, error) {
	return n.signatureRequestHandler.OnSignatureRequest(ctx, nodeID, requestID, signatureRequest)
}

// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/metrics"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	syncHandlers "github.com/ava-labs/subnet-evm/sync/handlers"
	syncStats "github.com/ava-labs/subnet-evm/sync/handlers/stats"
	"github.com/ava-labs/subnet-evm/trie"
	warpHandlers "github.com/ava-labs/subnet-evm/warp/handlers"
)

var _ message.RequestHandler = &networkHandler{}

type networkHandler struct {
	stateTrieLeafsRequestHandler *syncHandlers.LeafsRequestHandler
	blockRequestHandler          *syncHandlers.BlockRequestHandler
	codeRequestHandler           *syncHandlers.CodeRequestHandler
	signatureRequestHandler      warpHandlers.SignatureRequestHandler
}

// newNetworkHandler constructs the handler for serving network requests.
func newNetworkHandler(
	provider syncHandlers.SyncDataProvider,
	evmTrieDB *trie.Database,
	networkCodec codec.Manager,
) message.RequestHandler {
	syncStats := syncStats.NewHandlerStats(metrics.Enabled)
	return &networkHandler{
		// State sync handlers
		stateTrieLeafsRequestHandler: syncHandlers.NewLeafsRequestHandler(evmTrieDB, provider, networkCodec, syncStats),
		blockRequestHandler:          syncHandlers.NewBlockRequestHandler(provider, networkCodec, syncStats),
		codeRequestHandler:           syncHandlers.NewCodeRequestHandler(evmTrieDB.DiskDB(), networkCodec, syncStats),

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

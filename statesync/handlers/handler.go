// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

var _ message.RequestHandler = &syncHandler{}

type syncHandler struct {
	stateTrieLeafsRequestHandler  *LeafsRequestHandler
	atomicTrieLeafsRequestHandler *LeafsRequestHandler
	blockRequestHandler           *BlockRequestHandler
	codeRequestHandler            *CodeRequestHandler
}

func NewSyncHandler(
	stateTrieLeafsRequestHandler *LeafsRequestHandler,
	atomicTrieLeafsRequestHandler *LeafsRequestHandler,
	blockRequestHandler *BlockRequestHandler,
	codeRequestHandler *CodeRequestHandler,
) message.RequestHandler {
	return &syncHandler{
		stateTrieLeafsRequestHandler:  stateTrieLeafsRequestHandler,
		atomicTrieLeafsRequestHandler: atomicTrieLeafsRequestHandler,
		blockRequestHandler:           blockRequestHandler,
		codeRequestHandler:            codeRequestHandler,
	}
}

func (s *syncHandler) HandleStateTrieLeafsRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	return s.stateTrieLeafsRequestHandler.OnLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (s *syncHandler) HandleAtomicTrieLeafsRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	return s.atomicTrieLeafsRequestHandler.OnLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (s *syncHandler) HandleBlockRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error) {
	return s.blockRequestHandler.OnBlockRequest(ctx, nodeID, requestID, blockRequest)
}

func (s *syncHandler) HandleCodeRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
	return s.codeRequestHandler.OnCodeRequest(ctx, nodeID, requestID, codeRequest)
}

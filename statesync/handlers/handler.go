// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
)

var _ message.RequestHandler = &syncHandler{}

type syncHandler struct {
	trieLeafsRequestHandler *LeafsRequestHandler
	blockRequestHandler     *BlockRequestHandler
	codeRequestHandler      *CodeRequestHandler
}

func NewSyncHandler(
	trieLeafsRequestHandler *LeafsRequestHandler,
	blockRequestHandler *BlockRequestHandler,
	codeRequestHandler *CodeRequestHandler,
) message.RequestHandler {
	return &syncHandler{
		trieLeafsRequestHandler: trieLeafsRequestHandler,
		blockRequestHandler:     blockRequestHandler,
		codeRequestHandler:      codeRequestHandler,
	}
}

func (s *syncHandler) HandleTrieLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	return s.trieLeafsRequestHandler.OnLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (s *syncHandler) HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error) {
	return s.blockRequestHandler.OnBlockRequest(ctx, nodeID, requestID, blockRequest)
}

func (s *syncHandler) HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
	return s.codeRequestHandler.OnCodeRequest(ctx, nodeID, requestID, codeRequest)
}

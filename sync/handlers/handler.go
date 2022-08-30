// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/core/state/snapshot"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
	"github.com/ava-labs/subnet-evm/sync/handlers/stats"
	"github.com/ava-labs/subnet-evm/trie"
	"github.com/ethereum/go-ethereum/common"
)

var _ message.RequestHandler = &syncHandler{}

type BlockProvider interface {
	GetBlock(common.Hash, uint64) *types.Block
}

type SnapshotProvider interface {
	Snapshots() *snapshot.Tree
}

type SyncDataProvider interface {
	BlockProvider
	SnapshotProvider
}

type syncHandler struct {
	stateTrieLeafsRequestHandler *LeafsRequestHandler
	blockRequestHandler          *BlockRequestHandler
	codeRequestHandler           *CodeRequestHandler
}

// NewSyncHandler constructs the handler for serving state sync.
func NewSyncHandler(
	provider SyncDataProvider,
	evmTrieDB *trie.Database,
	networkCodec codec.Manager,
	stats stats.HandlerStats,
) message.RequestHandler {
	return &syncHandler{
		stateTrieLeafsRequestHandler: NewLeafsRequestHandler(evmTrieDB, provider, networkCodec, stats),
		blockRequestHandler:          NewBlockRequestHandler(provider, networkCodec, stats),
		codeRequestHandler:           NewCodeRequestHandler(evmTrieDB.DiskDB(), networkCodec, stats),
	}
}

func (s *syncHandler) HandleTrieLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, leafsRequest message.LeafsRequest) ([]byte, error) {
	return s.stateTrieLeafsRequestHandler.OnLeafsRequest(ctx, nodeID, requestID, leafsRequest)
}

func (s *syncHandler) HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, blockRequest message.BlockRequest) ([]byte, error) {
	return s.blockRequestHandler.OnBlockRequest(ctx, nodeID, requestID, blockRequest)
}

func (s *syncHandler) HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, codeRequest message.CodeRequest) ([]byte, error) {
	return s.codeRequestHandler.OnCodeRequest(ctx, nodeID, requestID, codeRequest)
}

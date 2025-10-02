// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/message"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

var (
	_ SyncClient     = (*TestClient)(nil)
	_ EthBlockParser = (*testBlockParser)(nil)
)

type TestClient struct {
	codec          codec.Manager
	leafsHandler   handlers.LeafRequestHandler
	leavesReceived int32
	codesHandler   *handlers.CodeRequestHandler
	codeReceived   int32
	blocksHandler  *handlers.BlockRequestHandler
	blocksReceived int32
	// GetLeafsIntercept is called on every GetLeafs request if set to a non-nil callback.
	// The returned response will be returned by TestClient to the caller.
	GetLeafsIntercept func(req message.LeafsRequest, res message.LeafsResponse) (message.LeafsResponse, error)
	// GetCodesIntercept is called on every GetCode request if set to a non-nil callback.
	// The returned response will be returned by TestClient to the caller.
	GetCodeIntercept func(hashes []common.Hash, codeBytes [][]byte) ([][]byte, error)
	// GetBlocksIntercept is called on every GetBlocks request if set to a non-nil callback.
	// The returned response will be returned by TestClient to the caller.
	GetBlocksIntercept func(blockReq message.BlockRequest, blocks types.Blocks) (types.Blocks, error)
}

func NewTestClient(
	codec codec.Manager,
	leafHandler handlers.LeafRequestHandler,
	codesHandler *handlers.CodeRequestHandler,
	blocksHandler *handlers.BlockRequestHandler,
) *TestClient {
	return &TestClient{
		codec:         codec,
		leafsHandler:  leafHandler,
		codesHandler:  codesHandler,
		blocksHandler: blocksHandler,
	}
}

func (ml *TestClient) GetLeafs(ctx context.Context, request message.LeafsRequest) (message.LeafsResponse, error) {
	response, err := ml.leafsHandler.OnLeafsRequest(ctx, ids.GenerateTestNodeID(), 1, request)
	if err != nil {
		return message.LeafsResponse{}, err
	}

	leafResponseIntf, numLeaves, err := parseLeafsResponse(ml.codec, request, response)
	if err != nil {
		return message.LeafsResponse{}, err
	}
	leafsResponse := leafResponseIntf.(message.LeafsResponse)
	if ml.GetLeafsIntercept != nil {
		leafsResponse, err = ml.GetLeafsIntercept(request, leafsResponse)
	}
	// Increment the number of leaves received by the test client
	atomic.AddInt32(&ml.leavesReceived, int32(numLeaves))
	return leafsResponse, err
}

func (ml *TestClient) LeavesReceived() int32 {
	return atomic.LoadInt32(&ml.leavesReceived)
}

func (ml *TestClient) GetCode(ctx context.Context, hashes []common.Hash) ([][]byte, error) {
	if ml.codesHandler == nil {
		panic("no code handler for test client")
	}
	request := message.CodeRequest{Hashes: hashes}
	response, err := ml.codesHandler.OnCodeRequest(ctx, ids.GenerateTestNodeID(), 1, request)
	if err != nil {
		return nil, err
	}

	codeBytesIntf, lenCode, err := parseCode(ml.codec, request, response)
	if err != nil {
		return nil, err
	}
	code := codeBytesIntf.([][]byte)
	if ml.GetCodeIntercept != nil {
		code, err = ml.GetCodeIntercept(hashes, code)
	}
	if err == nil {
		atomic.AddInt32(&ml.codeReceived, int32(lenCode))
	}
	return code, err
}

func (ml *TestClient) CodeReceived() int32 {
	return atomic.LoadInt32(&ml.codeReceived)
}

func (ml *TestClient) GetBlocks(ctx context.Context, blockHash common.Hash, height uint64, numParents uint16) ([]*types.Block, error) {
	if ml.blocksHandler == nil {
		panic("no blocks handler for test client")
	}
	request := message.BlockRequest{
		Hash:    blockHash,
		Height:  height,
		Parents: numParents,
	}
	response, err := ml.blocksHandler.OnBlockRequest(ctx, ids.GenerateTestNodeID(), 1, request)
	if err != nil {
		return nil, err
	}
	// Actual client retries until the context is canceled.
	if response == nil {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	client := &Client{blockParser: newTestBlockParser()} // Hack to avoid duplicate code
	blocksRes, numBlocks, err := client.parseBlocks(ml.codec, request, response)
	if err != nil {
		return nil, err
	}
	blocks := blocksRes.(types.Blocks)
	if ml.GetBlocksIntercept != nil {
		blocks, err = ml.GetBlocksIntercept(request, blocks)
	}
	atomic.AddInt32(&ml.blocksReceived, int32(numBlocks))
	return blocks, err
}

func (ml *TestClient) BlocksReceived() int32 {
	return atomic.LoadInt32(&ml.blocksReceived)
}

type testBlockParser struct{}

func newTestBlockParser() *testBlockParser {
	return &testBlockParser{}
}

func (*testBlockParser) ParseEthBlock(b []byte) (*types.Block, error) {
	block := new(types.Block)
	if err := rlp.DecodeBytes(b, block); err != nil {
		return nil, fmt.Errorf("%w: %w", errUnmarshalResponse, err)
	}

	return block, nil
}

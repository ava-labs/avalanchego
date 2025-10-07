// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

// JSONRPCClient for interacting with the jsonrpc API.
type JSONRPCClient struct {
	Requester rpc.EndpointRequester
}

// NewJSONRPCClient returns a Client for interacting with the jsonrpc API.
//
// The provided chain should be the chainID or an alias. Such as "P" for the
// P-Chain.
func NewJSONRPCClient(uri string, chain string) *JSONRPCClient {
	path := fmt.Sprintf(
		"%s/ext/%s/%s%s",
		uri,
		constants.ChainAliasPrefix,
		chain,
		httpPathEndpoint,
	)
	return &JSONRPCClient{
		Requester: rpc.NewEndpointRequester(path),
	}
}

// GetProposedHeight returns the P-chain height this node would propose in the
// next block.
func (j *JSONRPCClient) GetProposedHeight(ctx context.Context, options ...rpc.Option) (uint64, error) {
	res := &api.GetHeightResponse{}
	err := j.Requester.SendRequest(ctx, "proposervm.getProposedHeight", struct{}{}, res, options...)
	return uint64(res.Height), err
}

// GetCurrentEpoch returns the current epoch information.
func (j *JSONRPCClient) GetCurrentEpoch(ctx context.Context, options ...rpc.Option) (block.Epoch, error) {
	res := &GetEpochResponse{}
	err := j.Requester.SendRequest(ctx, "proposervm.getCurrentEpoch", struct{}{}, res, options...)
	if err != nil {
		return block.Epoch{}, err
	}

	return block.Epoch{
		PChainHeight: uint64(res.PChainHeight),
		Number:       uint64(res.Number),
		StartTime:    int64(res.StartTime),
	}, nil
}

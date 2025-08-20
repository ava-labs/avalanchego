// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var _ Client = (*client)(nil)

type Client interface {
	// GetProposedHeight returns the current height of this node's proposer VM.
	GetProposedHeight(ctx context.Context, options ...rpc.Option) (uint64, error)
	// GetEpoch returns the current epoch number, start time, and P-Chain height.
	GetEpoch(ctx context.Context, options ...rpc.Option) (block.PChainEpoch, error)
}

type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with the ProposerVM API.
// The provided blockchainName should be the blockchainID or an alias (e.g. "P" for the P-Chain).
func NewClient(uri string, blockchainName string) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri + fmt.Sprintf("/ext/bc/%s/proposervm", blockchainName)),
	}
}

func (c *client) GetProposedHeight(ctx context.Context, options ...rpc.Option) (uint64, error) {
	res := &api.GetHeightResponse{}
	err := c.requester.SendRequest(ctx, "proposervm.getProposedHeight", struct{}{}, res, options...)
	return uint64(res.Height), err
}

func (c *client) GetEpoch(ctx context.Context, options ...rpc.Option) (block.PChainEpoch, error) {
	res := &GetEpochResponse{}
	if err := c.requester.SendRequest(ctx, "proposervm.getEpoch", struct{}{}, res, options...); err != nil {
		return block.PChainEpoch{}, err
	}
	return block.PChainEpoch{
		Height:    uint64(res.PChainHeight),
		Number:    uint64(res.Number),
		StartTime: time.Unix(int64(res.StartTime), 0),
	}, nil
}

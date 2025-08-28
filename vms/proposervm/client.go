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

type Client struct {
	Requester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with the ProposerVM API.
// The provided blockchainName should be the blockchainID or an alias (e.g. "P" for the P-Chain).
func NewClient(uri string, blockchainName string) *Client {
	return &Client{
		Requester: rpc.NewEndpointRequester(uri + fmt.Sprintf("/ext/bc/%s/proposervm", blockchainName)),
	}
}

func (c *Client) GetProposedHeight(ctx context.Context, options ...rpc.Option) (uint64, error) {
	res := &api.GetHeightResponse{}
	err := c.Requester.SendRequest(ctx, "proposervm.getProposedHeight", struct{}{}, res, options...)
	return uint64(res.Height), err
}

func (c *Client) GetEpoch(ctx context.Context, options ...rpc.Option) (block.PChainEpoch, error) {
	res := &GetEpochResponse{}
	if err := c.Requester.SendRequest(ctx, "proposervm.getEpoch", struct{}{}, res, options...); err != nil {
		return block.PChainEpoch{}, err
	}
	return block.PChainEpoch{
		Height:    uint64(res.PChainHeight),
		Number:    uint64(res.Number),
		StartTime: time.Unix(int64(res.StartTime), 0),
	}, nil
}

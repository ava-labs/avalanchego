// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Client for interacting with the json API.
type Client struct {
	Requester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with the json API.
//
// The provided chain should be the chainID or an alias. Such as "P" for the
// P-Chain.
func NewClient(uri string, chain string) *Client {
	path := fmt.Sprintf(
		"%s/ext/%s/%s%s",
		uri,
		constants.ChainAliasPrefix,
		chain,
		HTTPPathEndpoint,
	)
	return &Client{
		Requester: rpc.NewEndpointRequester(path),
	}
}

// GetProposedHeight returns the P-chain height this node would propose in the
// next block.
func (c *Client) GetProposedHeight(ctx context.Context, options ...rpc.Option) (uint64, error) {
	res := &api.GetHeightResponse{}
	err := c.Requester.SendRequest(ctx, "proposervm.getProposedHeight", struct{}{}, res, options...)
	return uint64(res.Height), err
}

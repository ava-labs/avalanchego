// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/rpc"
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

// GetProposedHeight returns the current height of this node's proposer VM.
func (c *Client) GetProposedHeight(ctx context.Context, options ...rpc.Option) (uint64, error) {
	res := &api.GetHeightResponse{}
	err := c.Requester.SendRequest(ctx, "proposervm.getProposedHeight", struct{}{}, res, options...)
	return uint64(res.Height), err
}

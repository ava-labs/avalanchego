// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

var _ Client = (*client)(nil)

type Client interface {
	// GetProposedHeight returns the current height of this node's proposer VM.
	GetProposedHeight(ctx context.Context, options ...rpc.Option) (uint64, error)
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

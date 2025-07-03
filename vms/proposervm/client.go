// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

var _ Client = (*client)(nil)

type Client interface {
	// GetProposedHeight returns the current height of this node's proposer VM.
	GetProposedHeight(ctx context.Context, options ...rpc.Option) (uint64, error)
	// GetProposerBlockWrapper returns the ProposerVM block wrapper
	GetProposerBlockWrapper(ctx context.Context, proposerID ids.ID, options ...rpc.Option) ([]byte, error)
}

type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with the ProposerVM API.
// The provided blockchainName should be the blockchainID or an alias (e.g. "P" for the P-Chain).
func NewClient(uri string, blockchainName string) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri + fmt.Sprintf("/%s/proposervm", blockchainName)),
	}
}

func (c *client) GetProposedHeight(ctx context.Context, options ...rpc.Option) (uint64, error) {
	res := &api.GetHeightResponse{}
	err := c.requester.SendRequest(ctx, "proposervm.getProposedHeight", struct{}{}, res, options...)
	return uint64(res.Height), err
}

func (c *client) GetProposerBlockWrapper(ctx context.Context, proposerID ids.ID, options ...rpc.Option) ([]byte, error) {
	res := &api.FormattedBlock{}
	if err := c.requester.SendRequest(ctx, "proposervm.getBlock", &GetProposerBlockArgs{
		ProposerID: proposerID,
		Encoding:   formatting.Hex,
	}, res, options...); err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.Block)
}

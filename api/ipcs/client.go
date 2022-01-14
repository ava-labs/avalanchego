// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ipcs

import (
	"context"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ Client = &client{}

// Client interface for interacting with the IPCS endpoint
type Client interface {
	// PublishBlockchain requests the node to begin publishing consensus and decision events
	PublishBlockchain(ctx context.Context, chainID string) (*PublishBlockchainReply, error)
	// UnpublishBlockchain requests the node to stop publishing consensus and decision events
	UnpublishBlockchain(ctx context.Context, chainID string) (bool, error)
	// GetPublishedBlockchains requests the node to get blockchains being published
	GetPublishedBlockchains(ctx context.Context) ([]ids.ID, error)
}

// Client implementation for interacting with the IPCS endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with the IPCS endpoint
func NewClient(uri string) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/ipcs", "ipcs"),
	}
}

func (c *client) PublishBlockchain(ctx context.Context, blockchainID string) (*PublishBlockchainReply, error) {
	res := &PublishBlockchainReply{}
	err := c.requester.SendRequest(ctx, "publishBlockchain", &PublishBlockchainArgs{
		BlockchainID: blockchainID,
	}, res)
	return res, err
}

func (c *client) UnpublishBlockchain(ctx context.Context, blockchainID string) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "unpublishBlockchain", &UnpublishBlockchainArgs{
		BlockchainID: blockchainID,
	}, res)
	return res.Success, err
}

func (c *client) GetPublishedBlockchains(ctx context.Context) ([]ids.ID, error) {
	res := &GetPublishedBlockchainsReply{}
	err := c.requester.SendRequest(ctx, "getPublishedBlockchains", nil, res)
	return res.Chains, err
}

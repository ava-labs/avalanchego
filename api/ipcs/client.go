// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ipcs

import (
	"context"

	"github.com/chain4travel/caminogo/api"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/rpc"
)

// Interface compliance
var _ Client = &client{}

// Client interface for interacting with the IPCS endpoint
type Client interface {
	// PublishBlockchain requests the node to begin publishing consensus and decision events
	PublishBlockchain(ctx context.Context, chainID string, options ...rpc.Option) (*PublishBlockchainReply, error)
	// UnpublishBlockchain requests the node to stop publishing consensus and decision events
	UnpublishBlockchain(ctx context.Context, chainID string, options ...rpc.Option) (bool, error)
	// GetPublishedBlockchains requests the node to get blockchains being published
	GetPublishedBlockchains(ctx context.Context, options ...rpc.Option) ([]ids.ID, error)
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

func (c *client) PublishBlockchain(ctx context.Context, blockchainID string, options ...rpc.Option) (*PublishBlockchainReply, error) {
	res := &PublishBlockchainReply{}
	err := c.requester.SendRequest(ctx, "publishBlockchain", &PublishBlockchainArgs{
		BlockchainID: blockchainID,
	}, res, options...)
	return res, err
}

func (c *client) UnpublishBlockchain(ctx context.Context, blockchainID string, options ...rpc.Option) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "unpublishBlockchain", &UnpublishBlockchainArgs{
		BlockchainID: blockchainID,
	}, res, options...)
	return res.Success, err
}

func (c *client) GetPublishedBlockchains(ctx context.Context, options ...rpc.Option) ([]ids.ID, error) {
	res := &GetPublishedBlockchainsReply{}
	err := c.requester.SendRequest(ctx, "getPublishedBlockchains", nil, res, options...)
	return res.Chains, err
}

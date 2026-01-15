// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/ids"
)

type Client struct {
	client *rpc.Client
}

// NewClient returns a Client for interacting with EVM chain
func NewClient(uri, chain string) (*Client, error) {
	c, err := rpc.Dial(fmt.Sprintf("%s/ext/bc/%s/rpc", uri, chain))
	if err != nil {
		return nil, fmt.Errorf("failed to dial client: %w", err)
	}
	return &Client{
		client: c,
	}, nil
}

// GetMessage returns the Warp message associated with the given messageID.
func (c *Client) GetMessage(ctx context.Context, messageID ids.ID) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getMessage", messageID); err != nil {
		return nil, fmt.Errorf("call to warp_getMessage failed: %w", err)
	}
	return res, nil
}

// GetMessageSignature returns the BLS signature associated with the given messageID.
func (c *Client) GetMessageSignature(ctx context.Context, messageID ids.ID) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getMessageSignature", messageID); err != nil {
		return nil, fmt.Errorf("call to warp_getMessageSignature failed: %w", err)
	}
	return res, nil
}

// GetMessageAggregateSignature fetches the aggregate signature for the given messageID.
func (c *Client) GetMessageAggregateSignature(ctx context.Context, messageID ids.ID, quorumNum uint64, subnetID ids.ID) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getMessageAggregateSignature", messageID, quorumNum, subnetID); err != nil {
		return nil, fmt.Errorf("call to warp_getMessageAggregateSignature failed: %w", err)
	}
	return res, nil
}

// GetBlockSignature returns the BLS signature associated with the given blockID.
func (c *Client) GetBlockSignature(ctx context.Context, blockID ids.ID) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getBlockSignature", blockID); err != nil {
		return nil, fmt.Errorf("call to warp_getBlockSignature failed: %w", err)
	}
	return res, nil
}

// GetBlockAggregateSignature fetches the aggregate signature for the given blockID.
func (c *Client) GetBlockAggregateSignature(ctx context.Context, blockID ids.ID, quorumNum uint64, subnetID ids.ID) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getBlockAggregateSignature", blockID, quorumNum, subnetID); err != nil {
		return nil, fmt.Errorf("call to warp_getBlockAggregateSignature failed: %w", err)
	}
	return res, nil
}

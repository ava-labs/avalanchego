// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common/hexutil"

	"github.com/ava-labs/avalanchego/graft/evm/rpc"
	"github.com/ava-labs/avalanchego/ids"
)

var _ Client = (*client)(nil)

type Client interface {
	GetMessage(ctx context.Context, messageID ids.ID) ([]byte, error)
	GetMessageSignature(ctx context.Context, messageID ids.ID) ([]byte, error)
	GetMessageAggregateSignature(ctx context.Context, messageID ids.ID, quorumNum uint64, subnetIDStr string) ([]byte, error)
	GetBlockSignature(ctx context.Context, blockID ids.ID) ([]byte, error)
	GetBlockAggregateSignature(ctx context.Context, blockID ids.ID, quorumNum uint64, subnetIDStr string) ([]byte, error)
}

// client implementation for interacting with EVM [chain]
type client struct {
	client *rpc.Client
}

// NewClient returns a Client for interacting with EVM [chain]
func NewClient(uri, chain string) (Client, error) {
	innerClient, err := rpc.Dial(fmt.Sprintf("%s/ext/bc/%s/rpc", uri, chain))
	if err != nil {
		return nil, fmt.Errorf("failed to dial client. err: %w", err)
	}
	return &client{
		client: innerClient,
	}, nil
}

func (c *client) GetMessage(ctx context.Context, messageID ids.ID) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getMessage", messageID); err != nil {
		return nil, fmt.Errorf("call to warp_getMessage failed. err: %w", err)
	}
	return res, nil
}

func (c *client) GetMessageSignature(ctx context.Context, messageID ids.ID) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getMessageSignature", messageID); err != nil {
		return nil, fmt.Errorf("call to warp_getMessageSignature failed. err: %w", err)
	}
	return res, nil
}

func (c *client) GetMessageAggregateSignature(ctx context.Context, messageID ids.ID, quorumNum uint64, subnetIDStr string) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getMessageAggregateSignature", messageID, quorumNum, subnetIDStr); err != nil {
		return nil, fmt.Errorf("call to warp_getMessageAggregateSignature failed. err: %w", err)
	}
	return res, nil
}

func (c *client) GetBlockSignature(ctx context.Context, blockID ids.ID) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getBlockSignature", blockID); err != nil {
		return nil, fmt.Errorf("call to warp_getBlockSignature failed. err: %w", err)
	}
	return res, nil
}

func (c *client) GetBlockAggregateSignature(ctx context.Context, blockID ids.ID, quorumNum uint64, subnetIDStr string) ([]byte, error) {
	var res hexutil.Bytes
	if err := c.client.CallContext(ctx, &res, "warp_getBlockAggregateSignature", blockID, quorumNum, subnetIDStr); err != nil {
		return nil, fmt.Errorf("call to warp_getBlockAggregateSignature failed. err: %w", err)
	}
	return res, nil
}

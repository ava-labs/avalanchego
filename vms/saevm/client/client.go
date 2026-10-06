// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package client provides a unified client for interacting with [ethclient],
// [gethclient], and any custom methods for Avalanche EVMs.
package client

import (
	"context"
	"math/big"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/ethclient/gethclient"
	"github.com/ava-labs/libevm/rpc"

	ethereum "github.com/ava-labs/libevm"
)

type (
	ec = ethclient.Client
	gc = gethclient.Client
)

// Client unites [ethclient.Client] and [gethclient.Client] with additional methods.
type Client struct {
	*ec
	*gc

	client *rpc.Client
}

// Dial connects a client to the given URL.
func Dial(rawurl string) (*Client, error) {
	return DialContext(context.Background(), rawurl)
}

// DialContext connects a client to the given URL with context.
func DialContext(ctx context.Context, rawurl string) (*Client, error) {
	c, err := rpc.DialContext(ctx, rawurl)
	if err != nil {
		return nil, err
	}
	return New(c), nil
}

// New creates a [Client] that uses the given RPC client.
func New(c *rpc.Client) *Client {
	return &Client{
		ec:     ethclient.NewClient(c),
		gc:     gethclient.New(c),
		client: c,
	}
}

// EthClient returns the embedded [ethclient.Client].
func (c *Client) EthClient() *ethclient.Client {
	return c.ec
}

// GethClient returns the embedded [gethclient.Client].
func (c *Client) GethClient() *gethclient.Client {
	return c.gc
}

// Close closes the RPC client.
func (c *Client) Close() {
	c.ec.Close()
}

// CallContext invokes the given RPC method with the provided arguments and
// stores the result.
func (c *Client) CallContext(ctx context.Context, result any, method string, args ...any) error {
	return c.client.CallContext(ctx, result, method, args...)
}

// EthSubscribe calls [rpc.Client.EthSubscribe].
func (c *Client) EthSubscribe(ctx context.Context, channel any, args ...any) (ethereum.Subscription, error) {
	return c.client.EthSubscribe(ctx, channel, args...)
}

// CallContract invokes [ethclient.Client.CallContract]. For
// [gethclient.CallContract], use [Client.GethClient].
func (c *Client) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return c.ec.CallContract(ctx, msg, blockNumber)
}

// EstimateBaseFee tries to estimate the base fee for the next block if it were
// created immediately. There is no guarantee that this will be the base fee
// used in the next block or that the next base fee will be higher or lower than
// the returned value.
func (c *Client) EstimateBaseFee(ctx context.Context) (*big.Int, error) {
	var hex hexutil.Big
	err := c.client.CallContext(ctx, &hex, "eth_baseFee")
	if err != nil {
		return nil, err
	}
	return (*big.Int)(&hex), nil
}

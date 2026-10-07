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

// Disambiguation of the different client types to allow for embedding all in
// the same struct.
type (
	Eth struct {
		*ethclient.Client
	}
	Geth struct {
		*gethclient.Client
	}
	RPC struct {
		*rpc.Client
	}
)

// Client unites [ethclient.Client], [gethclient.Client], and [rpc.Client].
type Client struct {
	Eth
	Geth
	RPC
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
		Eth:  Eth{ethclient.NewClient(c)},
		Geth: Geth{gethclient.New(c)},
		RPC:  RPC{c},
	}
}

// Close closes the RPC client.
func (c *Client) Close() {
	c.Eth.Close()
}

// CallContract invokes [ethclient.Client.CallContract]. For
// [gethclient.Client.CallContract], use [Client.Geth].
func (c *Client) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return c.Eth.CallContract(ctx, msg, blockNumber)
}

// EstimateBaseFee tries to estimate the base fee for the next block if it were
// created immediately. There is no guarantee that this will be the base fee
// used in the next block or that the next base fee will be higher or lower than
// the returned value.
func (c *Client) EstimateBaseFee(ctx context.Context) (*big.Int, error) {
	var hex hexutil.Big
	err := c.CallContext(ctx, &hex, "eth_baseFee")
	if err != nil {
		return nil, err
	}
	return (*big.Int)(&hex), nil
}

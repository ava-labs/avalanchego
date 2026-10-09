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

	"github.com/ava-labs/avalanchego/vms/components/gas"

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

// Client unifies [ethclient.Client], [gethclient.Client] and [rpc.Client].
type Client struct {
	Eth
	Geth
	RPC
}

// Dial connects a client to the given URL with context.
func Dial(ctx context.Context, rawurl string) (*Client, error) {
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
	c.RPC.Close() // [ethclient.Client.Close] just closes this
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
func (c *Client) EstimateBaseFee(ctx context.Context) (gas.Price, error) {
	var bf hexutil.Uint64
	if err := c.CallContext(ctx, &bf, "eth_baseFee"); err != nil {
		return 0, err
	}
	return gas.Price(bf), nil
}

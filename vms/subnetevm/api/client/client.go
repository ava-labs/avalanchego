// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package client is the typed Go client for `vms/subnetevm`.
package client

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"

	subnetevmapi "github.com/ava-labs/avalanchego/vms/subnetevm/api"
	libevmrpc "github.com/ava-labs/libevm/rpc"
)

// Client talks to a `vms/subnetevm` chain. The standard Ethereum surface is
// available via the embedded [ethclient.Client]. Subnet-evm-specific methods
// are defined directly on `Client`.
//
// Construct with [NewClient] (preferred) or [NewClientWithURL]. Call [Close]
// when done.
type Client struct {
	*ethclient.Client

	ethRPC              *libevmrpc.Client
	validatorsRequester rpc.EndpointRequester
}

// NewClient dials the chain reachable at `<uri>/ext/bc/<chain>`.
func NewClient(uri, chain string) (*Client, error) {
	return NewClientWithURL(fmt.Sprintf("%s/ext/bc/%s", uri, chain))
}

// NewClientWithURL dials a chain at an explicit base URL. The `/rpc` and
// `/validators` suffixes are appended internally.
func NewClientWithURL(url string) (*Client, error) {
	ethRPC, err := libevmrpc.Dial(url + "/rpc")
	if err != nil {
		return nil, fmt.Errorf("dial %s/rpc: %w", url, err)
	}
	return &Client{
		Client:              ethclient.NewClient(ethRPC),
		ethRPC:              ethRPC,
		validatorsRequester: rpc.NewEndpointRequester(url + "/validators"),
	}, nil
}

// Close releases the underlying RPC connection.
func (c *Client) Close() {
	c.ethRPC.Close()
}

// GetCurrentValidators returns the configured validator set, optionally
// filtered to `nodeIDs`, augmented with per-validation uptime and connection
// status.
func (c *Client) GetCurrentValidators(ctx context.Context, nodeIDs []ids.NodeID, options ...rpc.Option) ([]subnetevmapi.CurrentValidator, error) {
	res := &subnetevmapi.GetCurrentValidatorsResponse{}
	err := c.validatorsRequester.SendRequest(
		ctx,
		"validators.getCurrentValidators",
		&subnetevmapi.GetCurrentValidatorsRequest{NodeIDs: nodeIDs},
		res,
		options...,
	)
	return res.Validators, err
}

// GetActiveRulesAt returns the active eth + avalanche rules and precompile
// activations at `blockTimestamp`. A nil `blockTimestamp` defaults to the
// current header time.
func (c *Client) GetActiveRulesAt(ctx context.Context, blockTimestamp *uint64) (subnetevmapi.ActiveRulesResult, error) {
	var res subnetevmapi.ActiveRulesResult
	err := c.ethRPC.CallContext(ctx, &res, "eth_getActiveRulesAt", blockTimestamp)
	return res, err
}

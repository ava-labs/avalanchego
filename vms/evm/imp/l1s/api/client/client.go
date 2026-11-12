// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package client is the typed Go client for `vms/evm/imp/l1s`.
package client

import (
	"context"

	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/evm/evmclient"

	l1api "github.com/ava-labs/avalanchego/vms/evm/imp/l1s/api"
)

// Client talks to a `vms/evm/imp/l1s` chain. It extends [evmclient.Client]
// with the L1-specific JSON-RPC methods.
//
// Construct with [Dial] or [New]. Call [Client.Close] when done.
type Client struct {
	*evmclient.Client
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
	return &Client{evmclient.New(c)}
}

// Uptime returns the uptime of `validationID` as observed by the node.
func (c *Client) Uptime(ctx context.Context, validationID ids.ID) (*l1api.Uptime, error) {
	res := &l1api.Uptime{}
	if err := c.CallContext(ctx, res, "validator_uptime", validationID); err != nil {
		return nil, err
	}
	return res, nil
}

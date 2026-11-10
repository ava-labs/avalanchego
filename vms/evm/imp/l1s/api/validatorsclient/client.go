// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package validatorsclient is the typed Go client for the validators API of
// `vms/evm/imp/l1s`.
package validatorsclient

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"

	l1api "github.com/ava-labs/avalanchego/vms/evm/imp/l1s/api"
)

// Client talks to the validators API of a `vms/evm/imp/l1s` chain.
type Client struct {
	requester rpc.EndpointRequester
}

// New returns a [Client] for the validators API served at `url`.
func New(url string) *Client {
	return &Client{requester: rpc.NewEndpointRequester(url)}
}

// GetCurrentValidators returns the configured validator set, optionally
// filtered to `nodeIDs`, augmented with per-validation uptime and connection
// status.
func (c *Client) GetCurrentValidators(ctx context.Context, nodeIDs []ids.NodeID, options ...rpc.Option) ([]l1api.CurrentValidator, error) {
	res := &l1api.GetCurrentValidatorsResponse{}
	err := c.requester.SendRequest(
		ctx,
		"validators.getCurrentValidators",
		&l1api.GetCurrentValidatorsRequest{NodeIDs: nodeIDs},
		res,
		options...,
	)
	return res.Validators, err
}

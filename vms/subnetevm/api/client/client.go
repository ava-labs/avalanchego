// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package client is a thin Go client for the subnetevm-local JSON-RPC
// handlers exposed by `vms/subnetevm` (currently just the
// `validators.getCurrentValidators` service). The wire shape is reused
// verbatim from the parent `api` package; this package owns only the
// transport.
package client

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
	subnetevmapi "github.com/ava-labs/avalanchego/vms/subnetevm/api"
)

var _ Client = (*client)(nil)

// Client is the public surface for talking to the `validators`
// JSON-RPC handler.
type Client interface {
	GetCurrentValidators(ctx context.Context, nodeIDs []ids.NodeID, options ...rpc.Option) ([]subnetevmapi.CurrentValidator, error)
}

type client struct {
	validatorsRequester rpc.EndpointRequester
}

// NewClient returns a Client for the `validators` handler of the EVM
// chain reachable at `<uri>/ext/bc/<chain>`.
func NewClient(uri, chain string) Client {
	return NewClientWithURL(fmt.Sprintf("%s/ext/bc/%s", uri, chain))
}

// NewClientWithURL returns a Client targeting an explicit base URL. The
// `/validators` suffix is appended internally.
func NewClientWithURL(url string) Client {
	return &client{
		validatorsRequester: rpc.NewEndpointRequester(url + "/validators"),
	}
}

// GetCurrentValidators returns the configured validator set, optionally
// filtered to `nodeIDs`, augmented with per-validation uptime and
// connection status.
func (c *client) GetCurrentValidators(ctx context.Context, nodeIDs []ids.NodeID, options ...rpc.Option) ([]subnetevmapi.CurrentValidator, error) {
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

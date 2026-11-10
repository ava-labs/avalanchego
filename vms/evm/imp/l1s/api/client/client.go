// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package client is the typed Go client for `vms/evm/imp/l1s`.
package client

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/evm/evmclient"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/api/validatorsclient"
)

// Disambiguation of the different client types to allow for embedding all in
// the same struct.
type (
	EVM        = evmclient.Client
	Validators = validatorsclient.Client
)

// Client talks to a `vms/evm/imp/l1s` chain. It unions the [evmclient.Client]
// with the [validatorsclient.Client].
//
// Construct with [Dial]. Call [Client.Close] when done.
type Client struct {
	*EVM
	*Validators
}

// Dial connects to a chain at an explicit base URL. The `/rpc` and
// `/validators` suffixes are appended internally.
func Dial(ctx context.Context, url string) (*Client, error) {
	c, err := evmclient.Dial(ctx, url+"/rpc")
	if err != nil {
		return nil, fmt.Errorf("dial %s/rpc: %w", url, err)
	}
	return &Client{
		EVM:        c,
		Validators: validatorsclient.New(url + "/validators"),
	}, nil
}

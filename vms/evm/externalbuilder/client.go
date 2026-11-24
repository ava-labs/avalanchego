// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package externalbuilder provides a JSON-RPC client for an external block
// builder, which proposes the leading transactions of blocks built by this
// node. See [sae.ExternalBuilder].
package externalbuilder

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/evm/sae"
)

// Method is the JSON-RPC method called by [Client.ProposeTxs]. Its parameter
// is the header of the block being built and its result is a list of
// binary-encoded transactions.
const Method = "builder_proposeTxs"

var (
	_ sae.ExternalBuilder = (*Client)(nil)

	errUndecodableTx = errors.New("undecodable transaction")
)

// Client calls an external block builder over JSON-RPC.
type Client struct {
	c *rpc.Client
}

// Dial connects to the external block builder at rawurl, which MAY be an HTTP,
// WebSocket or IPC endpoint.
func Dial(ctx context.Context, rawurl string) (*Client, error) {
	c, err := rpc.DialContext(ctx, rawurl)
	if err != nil {
		return nil, err
	}
	return &Client{c}, nil
}

// ProposeTxs implements [sae.ExternalBuilder].
func (c *Client) ProposeTxs(ctx context.Context, header *types.Header) ([]*types.Transaction, error) {
	var encoded []hexutil.Bytes
	if err := c.c.CallContext(ctx, &encoded, Method, header); err != nil {
		return nil, err
	}
	txs := make([]*types.Transaction, len(encoded))
	for i, b := range encoded {
		txs[i] = new(types.Transaction)
		if err := txs[i].UnmarshalBinary(b); err != nil {
			return nil, fmt.Errorf("%w %d: %w", errUndecodableTx, i, err)
		}
	}
	return txs, nil
}

// Close closes the connection to the external block builder.
func (c *Client) Close() {
	c.c.Close()
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package galiasreader

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"

	aliasreaderpb "github.com/ava-labs/avalanchego/buf/proto/pb/aliasreader"
)

var _ ids.AliaserReader = (*Client)(nil)

// Client implements alias lookups that talk over RPC.
type Client struct {
	client aliasreaderpb.AliasReaderClient
}

// NewClient returns an alias lookup instance connected to a remote alias lookup
// instance
func NewClient(client aliasreaderpb.AliasReaderClient) *Client {
	return &Client{client: client}
}

func (c *Client) Lookup(alias string) (ids.ID, error) {
	resp, err := c.client.Lookup(context.Background(), &aliasreaderpb.Alias{
		Alias: alias,
	})
	if err != nil {
		return ids.Empty, err
	}
	return ids.ToID(resp.Id)
}

func (c *Client) PrimaryAlias(id ids.ID) (string, error) {
	resp, err := c.client.PrimaryAlias(context.Background(), &aliasreaderpb.ID{
		Id: id[:],
	})
	if err != nil {
		return "", err
	}
	return resp.Alias, nil
}

func (c *Client) Aliases(id ids.ID) ([]string, error) {
	resp, err := c.client.Aliases(context.Background(), &aliasreaderpb.ID{
		Id: id[:],
	})
	if err != nil {
		return nil, err
	}
	return resp.Aliases, nil
}

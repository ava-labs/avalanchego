// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package galiasreader

import (
	"context"

	"github.com/chain4travel/caminogo/ids"

	aliasreaderpb "github.com/chain4travel/caminogo/proto/pb/aliasreader"
)

var _ ids.AliaserReader = &Client{}

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
		return ids.ID{}, err
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

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

	"github.com/chain4travel/caminogo/api/proto/galiasreaderproto"
	"github.com/chain4travel/caminogo/ids"
)

var _ ids.AliaserReader = &Client{}

// Client implements alias lookups that talk over RPC.
type Client struct {
	client galiasreaderproto.AliasReaderClient
}

// NewClient returns an alias lookup instance connected to a remote alias lookup
// instance
func NewClient(client galiasreaderproto.AliasReaderClient) *Client {
	return &Client{client: client}
}

func (c *Client) Lookup(alias string) (ids.ID, error) {
	resp, err := c.client.Lookup(context.Background(), &galiasreaderproto.Alias{
		Alias: alias,
	})
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ToID(resp.Id)
}

func (c *Client) PrimaryAlias(id ids.ID) (string, error) {
	resp, err := c.client.PrimaryAlias(context.Background(), &galiasreaderproto.ID{
		Id: id[:],
	})
	if err != nil {
		return "", err
	}
	return resp.Alias, nil
}

func (c *Client) Aliases(id ids.ID) ([]string, error) {
	resp, err := c.client.Aliases(context.Background(), &galiasreaderproto.ID{
		Id: id[:],
	})
	if err != nil {
		return nil, err
	}
	return resp.Aliases, nil
}

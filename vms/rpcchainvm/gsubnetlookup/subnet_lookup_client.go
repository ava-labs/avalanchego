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

package gsubnetlookup

import (
	"context"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow"

	subnetlookuppb "github.com/chain4travel/caminogo/proto/pb/subnetlookup"
)

var _ snow.SubnetLookup = &Client{}

// Client is a subnet lookup that talks over RPC.
type Client struct {
	client subnetlookuppb.SubnetLookupClient
}

// NewClient returns an alias lookup connected to a remote alias lookup
func NewClient(client subnetlookuppb.SubnetLookupClient) *Client {
	return &Client{client: client}
}

func (c *Client) SubnetID(chainID ids.ID) (ids.ID, error) {
	resp, err := c.client.SubnetID(context.Background(), &subnetlookuppb.SubnetIDRequest{
		ChainId: chainID[:],
	})
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ToID(resp.Id)
}

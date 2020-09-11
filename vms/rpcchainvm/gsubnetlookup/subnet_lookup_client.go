// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsubnetlookup

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/gsubnetlookup/gsubnetlookupproto"
)

var (
	_ snow.SubnetLookup = &Client{}
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct {
	client gsubnetlookupproto.SubnetLookupClient
}

// NewClient returns a alias lookup instance connected to a remote alias lookup instance
func NewClient(client gsubnetlookupproto.SubnetLookupClient) *Client {
	return &Client{client: client}
}

// SubnetID ...
func (c *Client) SubnetID(chainID ids.ID) (ids.ID, error) {
	resp, err := c.client.SubnetID(context.Background(), &gsubnetlookupproto.SubnetIDRequest{
		ChainID: chainID.Bytes(),
	})
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ToID(resp.Id)
}

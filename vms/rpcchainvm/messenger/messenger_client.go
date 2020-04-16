// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messenger

import (
	"context"

	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/vms/rpcchainvm/messenger/proto"
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct{ client proto.MessengerClient }

// NewClient returns a database instance connected to a remote database instance
func NewClient(client proto.MessengerClient) *Client {
	return &Client{client: client}
}

// Notify ...
func (c *Client) Notify(msg common.Message) error {
	_, err := c.client.Notify(context.Background(), &proto.NotifyRequest{
		Message: uint32(msg),
	})
	return err
}

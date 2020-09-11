// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messenger

import (
	"context"

	"github.com/ava-labs/avalanche-go/snow/engine/common"
	"github.com/ava-labs/avalanche-go/vms/rpcchainvm/messenger/messengerproto"
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct {
	client messengerproto.MessengerClient
}

// NewClient returns a database instance connected to a remote database instance
func NewClient(client messengerproto.MessengerClient) *Client {
	return &Client{client: client}
}

// Notify ...
func (c *Client) Notify(msg common.Message) error {
	_, err := c.client.Notify(context.Background(), &messengerproto.NotifyRequest{
		Message: uint32(msg),
	})
	return err
}

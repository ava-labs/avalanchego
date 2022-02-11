// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messenger

import (
	"context"

	"github.com/ava-labs/avalanchego/api/proto/messengerproto"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct {
	client messengerproto.MessengerClient
}

// NewClient returns a client that is connected to a remote channel
func NewClient(client messengerproto.MessengerClient) *Client {
	return &Client{client: client}
}

func (c *Client) Notify(msg common.Message) error {
	_, err := c.client.Notify(context.Background(), &messengerproto.NotifyRequest{
		Message: uint32(msg),
	})
	return err
}

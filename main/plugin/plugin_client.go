// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package plugin

import (
	"context"

	appproto "github.com/ava-labs/avalanchego/main/plugin/proto"

	"github.com/hashicorp/go-plugin"
)

type Client struct {
	client appproto.NodeClient
}

// NewServer returns a vm instance connected to a remote vm instance
func NewClient(node appproto.NodeClient, broker *plugin.GRPCBroker) *Client {
	return &Client{
		client: node,
	}
}

func (c *Client) Start() (int, error) {
	resp, err := c.client.Start(context.Background(), &appproto.StartRequest{})
	if err != nil {
		return 1, err
	}
	return int(resp.ExitCode), err
}

func (c *Client) Stop() (int, error) {
	resp, err := c.client.Stop(context.Background(), &appproto.StopRequest{})
	if err != nil {
		return 1, err
	}
	return int(resp.ExitCode), nil
}

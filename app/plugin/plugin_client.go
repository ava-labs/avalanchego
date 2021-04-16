// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package plugin

import (
	"context"

	appproto "github.com/ava-labs/avalanchego/app/plugin/proto"
)

type Client struct {
	client appproto.NodeClient
}

// NewServer returns a vm instance connected to a remote vm instance
func NewClient(node appproto.NodeClient) *Client {
	return &Client{
		client: node,
	}
}

// Blocks until the node is done shutting down.
// Returns the node's exit code.
func (c *Client) Start() (int, error) {
	resp, err := c.client.Start(context.Background(), &appproto.StartRequest{})
	if err != nil {
		return 1, err
	}
	return int(resp.ExitCode), err
}

// Blocks until the node is done shutting down.
func (c *Client) Stop() error {
	_, err := c.client.Stop(context.Background(), &appproto.StopRequest{})
	return err
}

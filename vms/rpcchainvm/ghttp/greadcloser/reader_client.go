// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package greadcloser

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/greadcloser/greadcloserproto"
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct{ client greadcloserproto.ReaderClient }

// NewClient returns a database instance connected to a remote database instance
func NewClient(client greadcloserproto.ReaderClient) *Client {
	return &Client{client: client}
}

// Read ...
func (c *Client) Read(p []byte) (int, error) {
	resp, err := c.client.Read(context.Background(), &greadcloserproto.ReadRequest{
		Length: int32(len(p)),
	})
	if err != nil {
		return 0, err
	}

	copy(p, resp.Read)

	if resp.Errored {
		err = errors.New(resp.Error)
	}
	return len(resp.Read), err
}

// Close ...
func (c *Client) Close() error {
	_, err := c.client.Close(context.Background(), &greadcloserproto.CloseRequest{})
	return err
}

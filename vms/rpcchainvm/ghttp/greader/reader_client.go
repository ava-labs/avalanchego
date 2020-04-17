// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package greader

import (
	"context"
	"errors"

	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/greader/proto"
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct{ client proto.ReaderClient }

// NewClient returns a database instance connected to a remote database instance
func NewClient(client proto.ReaderClient) *Client {
	return &Client{client: client}
}

// Read ...
func (c *Client) Read(p []byte) (int, error) {
	resp, err := c.client.Read(context.Background(), &proto.ReadRequest{
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

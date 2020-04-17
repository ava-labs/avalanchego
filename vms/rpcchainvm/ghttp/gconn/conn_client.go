// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gconn

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/ava-labs/gecko/utils/wrappers"
	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/gconn/proto"
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct {
	client  proto.ConnClient
	local   net.Addr
	remote  net.Addr
	toClose []io.Closer
}

// NewClient returns a database instance connected to a remote database instance
func NewClient(client proto.ConnClient, local, remote net.Addr, toClose ...io.Closer) *Client {
	return &Client{
		client:  client,
		local:   local,
		remote:  remote,
		toClose: toClose,
	}
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

// Write ...
func (c *Client) Write(b []byte) (int, error) {
	resp, err := c.client.Write(context.Background(), &proto.WriteRequest{
		Payload: b,
	})
	if err != nil {
		return 0, err
	}

	if resp.Errored {
		err = errors.New(resp.Error)
	}
	return int(resp.Length), err
}

// Close ...
func (c *Client) Close() error {
	_, err := c.client.Close(context.Background(), &proto.CloseRequest{})
	errs := wrappers.Errs{}
	errs.Add(err)
	for _, toClose := range c.toClose {
		errs.Add(toClose.Close())
	}
	return errs.Err
}

// LocalAddr ...
func (c *Client) LocalAddr() net.Addr { return c.local }

// RemoteAddr ...
func (c *Client) RemoteAddr() net.Addr { return c.remote }

// SetDeadline ...
func (c *Client) SetDeadline(t time.Time) error {
	bytes, err := t.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = c.client.SetDeadline(context.Background(), &proto.SetDeadlineRequest{
		Time: bytes,
	})
	return err
}

// SetReadDeadline ...
func (c *Client) SetReadDeadline(t time.Time) error {
	bytes, err := t.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = c.client.SetReadDeadline(context.Background(), &proto.SetReadDeadlineRequest{
		Time: bytes,
	})
	return err
}

// SetWriteDeadline ...
func (c *Client) SetWriteDeadline(t time.Time) error {
	bytes, err := t.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = c.client.SetWriteDeadline(context.Background(), &proto.SetWriteDeadlineRequest{
		Time: bytes,
	})
	return err
}

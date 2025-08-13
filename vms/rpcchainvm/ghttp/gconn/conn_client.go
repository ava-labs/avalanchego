// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gconn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/utils/wrappers"

	connpb "github.com/ava-labs/avalanchego/proto/pb/net/conn"
)

var _ net.Conn = (*Client)(nil)

// Client is an implementation of a connection that talks over RPC.
type Client struct {
	client  connpb.ConnClient
	local   net.Addr
	remote  net.Addr
	toClose []io.Closer
}

// NewClient returns a connection connected to a remote connection
func NewClient(client connpb.ConnClient, local, remote net.Addr, toClose ...io.Closer) *Client {
	return &Client{
		client:  client,
		local:   local,
		remote:  remote,
		toClose: toClose,
	}
}

func (c *Client) Read(p []byte) (int, error) {
	resp, err := c.client.Read(context.Background(), &connpb.ReadRequest{
		Length: int32(len(p)),
	})
	if err != nil {
		return 0, err
	}

	copy(p, resp.Read)

	if resp.Error != nil {
		switch resp.Error.ErrorCode {
		case connpb.ErrorCode_ERROR_CODE_EOF:
			err = io.EOF
		case connpb.ErrorCode_ERROR_CODE_OS_ERR_DEADLINE_EXCEEDED:
			err = fmt.Errorf("%w: %s", os.ErrDeadlineExceeded, resp.Error.Message)
		default:
			err = errors.New(resp.Error.Message)
		}
	}
	return len(resp.Read), err
}

func (c *Client) Write(b []byte) (int, error) {
	resp, err := c.client.Write(context.Background(), &connpb.WriteRequest{
		Payload: b,
	})
	if err != nil {
		return 0, err
	}

	if resp.Error != nil {
		err = errors.New(*resp.Error)
	}
	return int(resp.Length), err
}

func (c *Client) Close() error {
	_, err := c.client.Close(context.Background(), &emptypb.Empty{})
	errs := wrappers.Errs{}
	errs.Add(err)
	for _, toClose := range c.toClose {
		errs.Add(toClose.Close())
	}
	return errs.Err
}

func (c *Client) LocalAddr() net.Addr {
	return c.local
}

func (c *Client) RemoteAddr() net.Addr {
	return c.remote
}

func (c *Client) SetDeadline(t time.Time) error {
	bytes, err := t.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = c.client.SetDeadline(context.Background(), &connpb.SetDeadlineRequest{
		Time: bytes,
	})
	return err
}

func (c *Client) SetReadDeadline(t time.Time) error {
	bytes, err := t.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = c.client.SetReadDeadline(context.Background(), &connpb.SetDeadlineRequest{
		Time: bytes,
	})
	return err
}

func (c *Client) SetWriteDeadline(t time.Time) error {
	bytes, err := t.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = c.client.SetWriteDeadline(context.Background(), &connpb.SetDeadlineRequest{
		Time: bytes,
	})
	return err
}

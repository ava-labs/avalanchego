// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gresponsewriter

import (
	"bufio"
	"context"
	"net"
	"net/http"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/api/proto/gconnproto"
	"github.com/ava-labs/avalanchego/api/proto/greaderproto"
	"github.com/ava-labs/avalanchego/api/proto/gresponsewriterproto"
	"github.com/ava-labs/avalanchego/api/proto/gwriterproto"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gconn"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/greader"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gwriter"
)

var (
	_ http.ResponseWriter = &Client{}
	_ http.Flusher        = &Client{}
	_ http.Hijacker       = &Client{}
)

// Client is an http.ResponseWriter that talks over RPC.
type Client struct {
	client gresponsewriterproto.WriterClient
	header http.Header
	broker *plugin.GRPCBroker
}

// NewClient returns a response writer connected to a remote response writer
func NewClient(header http.Header, client gresponsewriterproto.WriterClient, broker *plugin.GRPCBroker) *Client {
	return &Client{
		client: client,
		header: header,
		broker: broker,
	}
}

func (c *Client) Header() http.Header { return c.header }

func (c *Client) Write(payload []byte) (int, error) {
	req := &gresponsewriterproto.WriteRequest{
		Headers: make([]*gresponsewriterproto.Header, 0, len(c.header)),
		Payload: payload,
	}
	for key, values := range c.header {
		req.Headers = append(req.Headers, &gresponsewriterproto.Header{
			Key:    key,
			Values: values,
		})
	}
	resp, err := c.client.Write(context.Background(), req)
	if err != nil {
		return 0, err
	}
	return int(resp.Written), nil
}

func (c *Client) WriteHeader(statusCode int) {
	req := &gresponsewriterproto.WriteHeaderRequest{
		Headers:    make([]*gresponsewriterproto.Header, 0, len(c.header)),
		StatusCode: int32(statusCode),
	}
	for key, values := range c.header {
		req.Headers = append(req.Headers, &gresponsewriterproto.Header{
			Key:    key,
			Values: values,
		})
	}
	// TODO: Is there a way to handle the error here?
	_, _ = c.client.WriteHeader(context.Background(), req)
}

func (c *Client) Flush() {
	// TODO: is there a way to handle the error here?
	_, _ = c.client.Flush(context.Background(), &emptypb.Empty{})
}

type addr struct {
	network string
	str     string
}

func (a *addr) Network() string { return a.network }
func (a *addr) String() string  { return a.str }

func (c *Client) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	resp, err := c.client.Hijack(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, nil, err
	}

	clientConn, err := c.broker.Dial(resp.ConnReadWriterServer)
	if err != nil {
		return nil, nil, err
	}

	conn := gconn.NewClient(
		gconnproto.NewConnClient(clientConn),
		&addr{
			network: resp.LocalNetwork,
			str:     resp.LocalString,
		},
		&addr{
			network: resp.RemoteNetwork,
			str:     resp.RemoteString,
		},
		clientConn,
	)

	reader := greader.NewClient(greaderproto.NewReaderClient(clientConn))
	writer := gwriter.NewClient(gwriterproto.NewWriterClient(clientConn))

	readWriter := bufio.NewReadWriter(
		bufio.NewReader(reader),
		bufio.NewWriter(writer),
	)

	return conn, readWriter, nil
}

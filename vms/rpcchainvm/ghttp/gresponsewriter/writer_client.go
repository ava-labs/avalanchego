// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gresponsewriter

import (
	"bufio"
	"context"
	"net"
	"net/http"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gconn"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gconn/gconnproto"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/greader"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/greader/greaderproto"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gresponsewriter/gresponsewriterproto"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gwriter"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/gwriter/gwriterproto"
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct {
	client gresponsewriterproto.WriterClient
	header http.Header
	broker *plugin.GRPCBroker
}

// NewClient returns a database instance connected to a remote database instance
func NewClient(header http.Header, client gresponsewriterproto.WriterClient, broker *plugin.GRPCBroker) *Client {
	return &Client{
		client: client,
		header: header,
		broker: broker,
	}
}

// Header ...
func (c *Client) Header() http.Header { return c.header }

// Write ...
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

// WriteHeader ...
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

// Flush ...
func (c *Client) Flush() {
	// TODO: is there a way to handle the error here?
	_, _ = c.client.Flush(context.Background(), &gresponsewriterproto.FlushRequest{})
}

type addr struct {
	network string
	str     string
}

func (a *addr) Network() string { return a.network }
func (a *addr) String() string  { return a.str }

// Hijack ...
func (c *Client) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	resp, err := c.client.Hijack(context.Background(), &gresponsewriterproto.HijackRequest{})
	if err != nil {
		return nil, nil, err
	}

	connConn, err := c.broker.Dial(resp.ConnServer)
	if err != nil {
		return nil, nil, err
	}

	readerConn, err := c.broker.Dial(resp.ReaderServer)
	if err != nil {
		// Ignore error closing resources to return original error
		_ = connConn.Close()
		return nil, nil, err
	}

	writerConn, err := c.broker.Dial(resp.WriterServer)
	if err != nil {
		// Ignore errors closing resources to return original error
		_ = connConn.Close()
		_ = readerConn.Close()
		return nil, nil, err
	}

	conn := gconn.NewClient(
		gconnproto.NewConnClient(connConn),
		&addr{
			network: resp.LocalNetwork,
			str:     resp.LocalString,
		},
		&addr{
			network: resp.RemoteNetwork,
			str:     resp.RemoteString,
		},
		connConn,
		readerConn,
		writerConn,
	)

	reader := greader.NewClient(greaderproto.NewReaderClient(readerConn))
	writer := gwriter.NewClient(gwriterproto.NewWriterClient(writerConn))

	readWriter := bufio.NewReadWriter(
		bufio.NewReader(reader),
		bufio.NewWriter(writer),
	)

	return conn, readWriter, nil
}

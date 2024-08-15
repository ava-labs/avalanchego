// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grequest

import (
	"context"
	"errors"
	"io"

	"github.com/ava-labs/avalanchego/proto/pb/http/request"
)

var _ io.Reader = (*Client)(nil)

func NewClient(ctx context.Context, client request.RequestClient) *Client {
	return &Client{
		ctx:    ctx,
		client: client,
	}
}

type Client struct {
	ctx    context.Context
	client request.RequestClient
}

func (c *Client) Read(p []byte) (n int, err error) {
	reply, err := c.client.Body(c.ctx, &request.BodyRequest{
		N: uint32(len(p)),
	})
	if errors.Is(err, io.EOF) {
		return 0, nil
	}

	if err != nil {
		return 0, err
	}

	return copy(p, reply.Body), nil
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grequest

import (
	"context"
	"errors"
	"io"

	"github.com/ava-labs/avalanchego/proto/pb/io/reader"
)

var _ io.Reader = (*Client)(nil)

func NewClient(ctx context.Context, client reader.ReaderClient) *Client {
	return &Client{
		ctx:    ctx,
		client: client,
	}
}

type Client struct {
	ctx    context.Context
	client reader.ReaderClient
}

func (c *Client) Read(p []byte) (int, error) {
	response, err := c.client.Read(
		c.ctx,
		&reader.ReadRequest{Length: int32(len(p))},
	)
	if err != nil {
		return 0, err
	}

	copy(p, response.Read)

	if response.Error != nil {
		err = errors.New(*response.Error)
	}

	return len(response.Read), nil
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ghttp

import (
	"context"
	"errors"
	"io"

	"github.com/ava-labs/avalanchego/proto/pb/io/reader"
)

var (
	_ reader.ReaderServer = (*bodyServer)(nil)
	_ io.Reader           = (*bodyClient)(nil)
)

type bodyServer struct {
	reader.UnimplementedReaderServer
	body io.Reader
}

func (b *bodyServer) Read(_ context.Context, readRequest *reader.ReadRequest) (*reader.ReadResponse, error) {
	body := make([]byte, readRequest.Length)
	n, err := b.body.Read(body)

	response := &reader.ReadResponse{
		Read: body[:n],
	}

	if err != nil {
		errStr := err.Error()
		response.Error = &errStr
	}

	return response, nil
}

type bodyClient struct {
	ctx    context.Context
	client reader.ReaderClient
}

func (b *bodyClient) Read(p []byte) (int, error) {
	response, err := b.client.Read(
		b.ctx,
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

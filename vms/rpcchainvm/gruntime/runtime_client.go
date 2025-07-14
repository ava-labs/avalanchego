// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gruntime

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"

	pb "github.com/ava-labs/avalanchego/buf/proto/pb/vm/runtime"
)

var _ runtime.Initializer = (*Client)(nil)

// Client is a VM runtime initializer.
type Client struct {
	client pb.RuntimeClient
}

func NewClient(client pb.RuntimeClient) *Client {
	return &Client{client: client}
}

func (c *Client) Initialize(ctx context.Context, protocolVersion uint, vmAddr string) error {
	_, err := c.client.Initialize(ctx, &pb.InitializeRequest{
		ProtocolVersion: uint32(protocolVersion),
		Addr:            vmAddr,
	})
	return err
}

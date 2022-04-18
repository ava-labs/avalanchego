// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"

	"github.com/ava-labs/avalanchego/utils/rpc"
)

var _ StaticClient = &staticClient{}

// StaticClient for interacting with the AVM static api
type StaticClient interface {
	BuildGenesis(ctx context.Context, args *BuildGenesisArgs, options ...rpc.Option) (*BuildGenesisReply, error)
}

// staticClient is an implementation of an AVM client for interacting with the
// avm static api
type staticClient struct {
	requester rpc.EndpointRequester
}

// NewClient returns an AVM client for interacting with the avm static api
func NewStaticClient(uri string) StaticClient {
	return &staticClient{
		requester: rpc.NewEndpointRequester(uri, "/ext/vm/avm", "avm"),
	}
}

func (c *staticClient) BuildGenesis(ctx context.Context, args *BuildGenesisArgs, options ...rpc.Option) (resp *BuildGenesisReply, err error) {
	resp = &BuildGenesisReply{}
	err = c.requester.SendRequest(ctx, "buildGenesis", args, resp, options...)
	return resp, err
}

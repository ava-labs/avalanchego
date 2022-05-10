// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"

	"github.com/ava-labs/avalanchego/utils/rpc"

	"github.com/ava-labs/avalanchego/vms/platformvm/api"
)

var _ StaticClient = &staticClient{}

// StaticClient for interacting with the platformvm static api
type StaticClient interface {
	BuildGenesis(
		ctx context.Context,
		args *api.BuildGenesisArgs,
		options ...rpc.Option,
	) (*api.BuildGenesisReply, error)
}

// staticClient is an implementation of a platformvm client for interacting with
// the platformvm static api
type staticClient struct {
	requester rpc.EndpointRequester
}

// NewClient returns a platformvm client for interacting with the platformvm static api
func NewStaticClient(uri string) StaticClient {
	return &staticClient{
		requester: rpc.NewEndpointRequester(uri, "/ext/vm/avm", "avm"),
	}
}

func (c *staticClient) BuildGenesis(
	ctx context.Context,
	args *api.BuildGenesisArgs,
	options ...rpc.Option,
) (resp *api.BuildGenesisReply, err error) {
	resp = &api.BuildGenesisReply{}
	err = c.requester.SendRequest(ctx, "buildGenesis", args, resp, options...)
	return resp, err
}

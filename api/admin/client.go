// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"context"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ Client = &client{}

// Client interface for the Avalanche Platform Info API Endpoint
type Client interface {
	StartCPUProfiler(context.Context) (bool, error)
	StopCPUProfiler(context.Context) (bool, error)
	MemoryProfile(context.Context) (bool, error)
	LockProfile(context.Context) (bool, error)
	Alias(ctx context.Context, endpoint string, alias string) (bool, error)
	AliasChain(ctx context.Context, chainID string, alias string) (bool, error)
	GetChainAliases(ctx context.Context, chainID string) ([]string, error)
	Stacktrace(context.Context) (bool, error)
}

// Client implementation for the Avalanche Platform Info API Endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a new Info API Client
func NewClient(uri string) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/admin", "admin"),
	}
}

func (c *client) StartCPUProfiler(ctx context.Context) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "startCPUProfiler", struct{}{}, res)
	return res.Success, err
}

func (c *client) StopCPUProfiler(ctx context.Context) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "stopCPUProfiler", struct{}{}, res)
	return res.Success, err
}

func (c *client) MemoryProfile(ctx context.Context) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "memoryProfile", struct{}{}, res)
	return res.Success, err
}

func (c *client) LockProfile(ctx context.Context) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "lockProfile", struct{}{}, res)
	return res.Success, err
}

func (c *client) Alias(ctx context.Context, endpoint, alias string) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "alias", &AliasArgs{
		Endpoint: endpoint,
		Alias:    alias,
	}, res)
	return res.Success, err
}

func (c *client) AliasChain(ctx context.Context, chain, alias string) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "aliasChain", &AliasChainArgs{
		Chain: chain,
		Alias: alias,
	}, res)
	return res.Success, err
}

func (c *client) GetChainAliases(ctx context.Context, chain string) ([]string, error) {
	res := &GetChainAliasesReply{}
	err := c.requester.SendRequest(ctx, "getChainAliases", &GetChainAliasesArgs{
		Chain: chain,
	}, res)
	return res.Aliases, err
}

func (c *client) Stacktrace(ctx context.Context) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest(ctx, "stacktrace", struct{}{}, res)
	return res.Success, err
}

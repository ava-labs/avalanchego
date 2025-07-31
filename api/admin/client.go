// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"context"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database/rpcdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

type Client struct {
	Requester rpc.EndpointRequester
}

func NewClient(uri string) *Client {
	return &Client{Requester: rpc.NewEndpointRequester(
		uri + "/ext/admin",
	)}
}

func (c *Client) StartCPUProfiler(ctx context.Context, options ...rpc.Option) error {
	return c.Requester.SendRequest(ctx, "admin.startCPUProfiler", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *Client) StopCPUProfiler(ctx context.Context, options ...rpc.Option) error {
	return c.Requester.SendRequest(ctx, "admin.stopCPUProfiler", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *Client) MemoryProfile(ctx context.Context, options ...rpc.Option) error {
	return c.Requester.SendRequest(ctx, "admin.memoryProfile", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *Client) LockProfile(ctx context.Context, options ...rpc.Option) error {
	return c.Requester.SendRequest(ctx, "admin.lockProfile", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *Client) Alias(ctx context.Context, endpoint, alias string, options ...rpc.Option) error {
	return c.Requester.SendRequest(ctx, "admin.alias", &AliasArgs{
		Endpoint: endpoint,
		Alias:    alias,
	}, &api.EmptyReply{}, options...)
}

func (c *Client) AliasChain(ctx context.Context, chain, alias string, options ...rpc.Option) error {
	return c.Requester.SendRequest(ctx, "admin.aliasChain", &AliasChainArgs{
		Chain: chain,
		Alias: alias,
	}, &api.EmptyReply{}, options...)
}

func (c *Client) GetChainAliases(ctx context.Context, chain string, options ...rpc.Option) ([]string, error) {
	res := &GetChainAliasesReply{}
	err := c.Requester.SendRequest(ctx, "admin.getChainAliases", &GetChainAliasesArgs{
		Chain: chain,
	}, res, options...)
	return res.Aliases, err
}

func (c *Client) Stacktrace(ctx context.Context, options ...rpc.Option) error {
	return c.Requester.SendRequest(ctx, "admin.stacktrace", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *Client) LoadVMs(ctx context.Context, options ...rpc.Option) (map[ids.ID][]string, map[ids.ID]string, error) {
	res := &LoadVMsReply{}
	err := c.Requester.SendRequest(ctx, "admin.loadVMs", struct{}{}, res, options...)
	return res.NewVMs, res.FailedVMs, err
}

func (c *Client) SetLoggerLevel(
	ctx context.Context,
	loggerName,
	logLevel,
	displayLevel string,
	options ...rpc.Option,
) (map[string]LogAndDisplayLevels, error) {
	var (
		logLevelArg     logging.Level
		displayLevelArg logging.Level
		err             error
	)
	if len(logLevel) > 0 {
		logLevelArg, err = logging.ToLevel(logLevel)
		if err != nil {
			return nil, err
		}
	}
	if len(displayLevel) > 0 {
		displayLevelArg, err = logging.ToLevel(displayLevel)
		if err != nil {
			return nil, err
		}
	}
	res := &LoggerLevelReply{}
	err = c.Requester.SendRequest(ctx, "admin.setLoggerLevel", &SetLoggerLevelArgs{
		LoggerName:   loggerName,
		LogLevel:     &logLevelArg,
		DisplayLevel: &displayLevelArg,
	}, res, options...)
	return res.LoggerLevels, err
}

func (c *Client) GetLoggerLevel(
	ctx context.Context,
	loggerName string,
	options ...rpc.Option,
) (map[string]LogAndDisplayLevels, error) {
	res := &LoggerLevelReply{}
	err := c.Requester.SendRequest(ctx, "admin.getLoggerLevel", &GetLoggerLevelArgs{
		LoggerName: loggerName,
	}, res, options...)
	return res.LoggerLevels, err
}

func (c *Client) GetConfig(ctx context.Context, options ...rpc.Option) (interface{}, error) {
	var res interface{}
	err := c.Requester.SendRequest(ctx, "admin.getConfig", struct{}{}, &res, options...)
	return res, err
}

func (c *Client) DBGet(ctx context.Context, key []byte, options ...rpc.Option) ([]byte, error) {
	keyStr, err := formatting.Encode(formatting.HexNC, key)
	if err != nil {
		return nil, err
	}

	res := &DBGetReply{}
	err = c.Requester.SendRequest(ctx, "admin.dbGet", &DBGetArgs{
		Key: keyStr,
	}, res, options...)
	if err != nil {
		return nil, err
	}

	if err := rpcdb.ErrEnumToError[res.ErrorCode]; err != nil {
		return nil, err
	}
	return formatting.Decode(formatting.HexNC, res.Value)
}

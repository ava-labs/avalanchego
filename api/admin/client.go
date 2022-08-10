// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

var _ Client = &client{}

// Client interface for the Avalanche Platform Info API Endpoint
type Client interface {
	StartCPUProfiler(context.Context, ...rpc.Option) error
	StopCPUProfiler(context.Context, ...rpc.Option) error
	MemoryProfile(context.Context, ...rpc.Option) error
	LockProfile(context.Context, ...rpc.Option) error
	Alias(ctx context.Context, endpoint string, alias string, options ...rpc.Option) error
	AliasChain(ctx context.Context, chainID string, alias string, options ...rpc.Option) error
	GetChainAliases(ctx context.Context, chainID string, options ...rpc.Option) ([]string, error)
	Stacktrace(context.Context, ...rpc.Option) error
	LoadVMs(context.Context, ...rpc.Option) (map[ids.ID][]string, map[ids.ID]string, error)
	SetLoggerLevel(ctx context.Context, loggerName, logLevel, displayLevel string, options ...rpc.Option) error
	GetLoggerLevel(ctx context.Context, loggerName string, options ...rpc.Option) (map[string]LogAndDisplayLevels, error)
	GetConfig(ctx context.Context, options ...rpc.Option) (interface{}, error)
}

// Client implementation for the Avalanche Platform Info API Endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a new Info API Client
func NewClient(uri string) Client {
	return &client{requester: rpc.NewEndpointRequester(
		uri+"/ext/admin",
		"admin",
	)}
}

func (c *client) StartCPUProfiler(ctx context.Context, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "startCPUProfiler", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *client) StopCPUProfiler(ctx context.Context, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "stopCPUProfiler", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *client) MemoryProfile(ctx context.Context, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "memoryProfile", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *client) LockProfile(ctx context.Context, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "lockProfile", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *client) Alias(ctx context.Context, endpoint, alias string, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "alias", &AliasArgs{
		Endpoint: endpoint,
		Alias:    alias,
	}, &api.EmptyReply{}, options...)
}

func (c *client) AliasChain(ctx context.Context, chain, alias string, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "aliasChain", &AliasChainArgs{
		Chain: chain,
		Alias: alias,
	}, &api.EmptyReply{}, options...)
}

func (c *client) GetChainAliases(ctx context.Context, chain string, options ...rpc.Option) ([]string, error) {
	res := &GetChainAliasesReply{}
	err := c.requester.SendRequest(ctx, "getChainAliases", &GetChainAliasesArgs{
		Chain: chain,
	}, res, options...)
	return res.Aliases, err
}

func (c *client) Stacktrace(ctx context.Context, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "stacktrace", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *client) LoadVMs(ctx context.Context, options ...rpc.Option) (map[ids.ID][]string, map[ids.ID]string, error) {
	res := &LoadVMsReply{}
	err := c.requester.SendRequest(ctx, "loadVMs", struct{}{}, res, options...)
	return res.NewVMs, res.FailedVMs, err
}

func (c *client) SetLoggerLevel(
	ctx context.Context,
	loggerName,
	logLevel,
	displayLevel string,
	options ...rpc.Option,
) error {
	var (
		logLevelArg     logging.Level
		displayLevelArg logging.Level
		err             error
	)
	if len(logLevel) > 0 {
		logLevelArg, err = logging.ToLevel(logLevel)
		if err != nil {
			return fmt.Errorf("couldn't parse %q to log level", logLevel)
		}
	}
	if len(displayLevel) > 0 {
		displayLevelArg, err = logging.ToLevel(displayLevel)
		if err != nil {
			return fmt.Errorf("couldn't parse %q to log level", displayLevel)
		}
	}
	return c.requester.SendRequest(ctx, "setLoggerLevel", &SetLoggerLevelArgs{
		LoggerName:   loggerName,
		LogLevel:     &logLevelArg,
		DisplayLevel: &displayLevelArg,
	}, &api.EmptyReply{}, options...)
}

func (c *client) GetLoggerLevel(
	ctx context.Context,
	loggerName string,
	options ...rpc.Option,
) (map[string]LogAndDisplayLevels, error) {
	res := &GetLoggerLevelReply{}
	err := c.requester.SendRequest(ctx, "getLoggerLevel", &GetLoggerLevelArgs{
		LoggerName: loggerName,
	}, res, options...)
	return res.LoggerLevels, err
}

func (c *client) GetConfig(ctx context.Context, options ...rpc.Option) (interface{}, error) {
	var res interface{}
	err := c.requester.SendRequest(ctx, "getConfig", struct{}{}, &res, options...)
	return res, err
}

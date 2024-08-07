// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

var _ Client = (*client)(nil)

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
	GetNodeSigner(ctx context.Context, _ string, options ...rpc.Option) (*GetNodeSignerReply, error)
}

// Client implementation for the Avalanche Platform Info API Endpoint
type client struct {
	requester rpc.EndpointRequester
	secret    string
}

// NewClient returns a new Info API Client
func NewClient(uri string, secret string) Client {
	return &client{requester: rpc.NewEndpointRequester(
		uri + "/ext/admin",
	), secret: secret}
}

func (c *client) StartCPUProfiler(ctx context.Context, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "admin.startCPUProfiler", Secret{c.secret}, &api.EmptyReply{}, options...)
}

func (c *client) StopCPUProfiler(ctx context.Context, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "admin.stopCPUProfiler", Secret{c.secret}, &api.EmptyReply{}, options...)
}

func (c *client) MemoryProfile(ctx context.Context, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "admin.memoryProfile", Secret{c.secret}, &api.EmptyReply{}, options...)
}

func (c *client) LockProfile(ctx context.Context, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "admin.lockProfile", Secret{c.secret}, &api.EmptyReply{}, options...)
}

func (c *client) Alias(ctx context.Context, endpoint, alias string, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "admin.alias", &AliasArgs{
		Secret:   Secret{c.secret},
		Endpoint: endpoint,
		Alias:    alias,
	}, &api.EmptyReply{}, options...)
}

func (c *client) AliasChain(ctx context.Context, chain, alias string, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "admin.aliasChain", &AliasChainArgs{
		Secret: Secret{c.secret},
		Chain:  chain,
		Alias:  alias,
	}, &api.EmptyReply{}, options...)
}

func (c *client) GetChainAliases(ctx context.Context, chain string, options ...rpc.Option) ([]string, error) {
	res := &GetChainAliasesReply{}
	err := c.requester.SendRequest(ctx, "admin.getChainAliases", &GetChainAliasesArgs{
		Secret: Secret{c.secret},
		Chain:  chain,
	}, res, options...)
	return res.Aliases, err
}

func (c *client) Stacktrace(ctx context.Context, options ...rpc.Option) error {
	return c.requester.SendRequest(ctx, "admin.stacktrace", Secret{c.secret}, &api.EmptyReply{}, options...)
}

func (c *client) LoadVMs(ctx context.Context, options ...rpc.Option) (map[ids.ID][]string, map[ids.ID]string, error) {
	res := &LoadVMsReply{}
	err := c.requester.SendRequest(ctx, "admin.loadVMs", Secret{c.secret}, res, options...)
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
	return c.requester.SendRequest(ctx, "admin.setLoggerLevel", &SetLoggerLevelArgs{
		Secret:       Secret{c.secret},
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
	err := c.requester.SendRequest(ctx, "admin.getLoggerLevel", &GetLoggerLevelArgs{
		Secret:     Secret{c.secret},
		LoggerName: loggerName,
	}, res, options...)
	return res.LoggerLevels, err
}

func (c *client) GetConfig(ctx context.Context, options ...rpc.Option) (interface{}, error) {
	var res interface{}
	err := c.requester.SendRequest(ctx, "admin.getConfig", Secret{c.secret}, &res, options...)
	return res, err
}

func (c *client) GetNodeSigner(ctx context.Context, _ string, options ...rpc.Option) (*GetNodeSignerReply, error) {
	res := &GetNodeSignerReply{}
	err := c.requester.SendRequest(ctx, "getNodeSigner", Secret{c.secret}, res, options...)
	return res, err
}

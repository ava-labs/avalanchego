// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"context"
	"fmt"

	"golang.org/x/exp/slog"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/graft/evm/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ Client = (*client)(nil)

type CurrentValidator struct {
	ValidationID     ids.ID     `json:"validationID"`
	NodeID           ids.NodeID `json:"nodeID"`
	Weight           uint64     `json:"weight"`
	StartTimestamp   uint64     `json:"startTimestamp"`
	IsActive         bool       `json:"isActive"`
	IsL1Validator    bool       `json:"isL1Validator"`
	IsConnected      bool       `json:"isConnected"`
	UptimePercentage float32    `json:"uptimePercentage"`
	UptimeSeconds    uint64     `json:"uptimeSeconds"`
}

// Client interface for interacting with EVM [chain]
type Client interface {
	StartCPUProfiler(ctx context.Context, options ...rpc.Option) error
	StopCPUProfiler(ctx context.Context, options ...rpc.Option) error
	MemoryProfile(ctx context.Context, options ...rpc.Option) error
	LockProfile(ctx context.Context, options ...rpc.Option) error
	SetLogLevel(ctx context.Context, level slog.Level, options ...rpc.Option) error
	GetVMConfig(ctx context.Context, options ...rpc.Option) (*config.L1Config, error)
	GetCurrentValidators(ctx context.Context, nodeIDs []ids.NodeID, options ...rpc.Option) ([]CurrentValidator, error)
}

// Client implementation for interacting with EVM [chain]
type client struct {
	adminRequester      rpc.EndpointRequester
	validatorsRequester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with EVM [chain]
func NewClient(uri, chain string) Client {
	requestURI := fmt.Sprintf("%s/ext/bc/%s", uri, chain)
	return NewClientWithURL(requestURI)
}

// NewClientWithURL returns a Client for interacting with EVM [chain]
func NewClientWithURL(url string) Client {
	return &client{
		adminRequester: rpc.NewEndpointRequester(
			url + "/admin",
		),
		validatorsRequester: rpc.NewEndpointRequester(
			url + "/validators",
		),
	}
}

func (c *client) StartCPUProfiler(ctx context.Context, options ...rpc.Option) error {
	return c.adminRequester.SendRequest(ctx, "admin.startCPUProfiler", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *client) StopCPUProfiler(ctx context.Context, options ...rpc.Option) error {
	return c.adminRequester.SendRequest(ctx, "admin.stopCPUProfiler", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *client) MemoryProfile(ctx context.Context, options ...rpc.Option) error {
	return c.adminRequester.SendRequest(ctx, "admin.memoryProfile", struct{}{}, &api.EmptyReply{}, options...)
}

func (c *client) LockProfile(ctx context.Context, options ...rpc.Option) error {
	return c.adminRequester.SendRequest(ctx, "admin.lockProfile", struct{}{}, &api.EmptyReply{}, options...)
}

type SetLogLevelArgs struct {
	Level string `json:"level"`
}

// SetLogLevel dynamically sets the log level for the C Chain
func (c *client) SetLogLevel(ctx context.Context, level slog.Level, options ...rpc.Option) error {
	return c.adminRequester.SendRequest(ctx, "admin.setLogLevel", &SetLogLevelArgs{
		Level: level.String(),
	}, &api.EmptyReply{}, options...)
}

type ConfigReply struct {
	Config *config.L1Config `json:"config"`
}

// GetVMConfig returns the current config of the VM
func (c *client) GetVMConfig(ctx context.Context, options ...rpc.Option) (*config.L1Config, error) {
	res := &ConfigReply{}
	err := c.adminRequester.SendRequest(ctx, "admin.getVMConfig", struct{}{}, res, options...)
	return res.Config, err
}

type GetCurrentValidatorsRequest struct {
	NodeIDs []ids.NodeID `json:"nodeIDs"`
}

type GetCurrentValidatorsResponse struct {
	Validators []CurrentValidator `json:"validators"`
}

// GetCurrentValidators returns the current validators
func (c *client) GetCurrentValidators(ctx context.Context, nodeIDs []ids.NodeID, options ...rpc.Option) ([]CurrentValidator, error) {
	res := &GetCurrentValidatorsResponse{}
	err := c.validatorsRequester.SendRequest(ctx, "validators.getCurrentValidators", &GetCurrentValidatorsRequest{
		NodeIDs: nodeIDs,
	}, res, options...)
	return res.Validators, err
}

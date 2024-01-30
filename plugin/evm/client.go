// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ethereum/go-ethereum/log"
)

// Interface compliance
var _ Client = (*client)(nil)

// Client interface for interacting with EVM [chain]
type Client interface {
	StartCPUProfiler(ctx context.Context, options ...rpc.Option) error
	StopCPUProfiler(ctx context.Context, options ...rpc.Option) error
	MemoryProfile(ctx context.Context, options ...rpc.Option) error
	LockProfile(ctx context.Context, options ...rpc.Option) error
	SetLogLevel(ctx context.Context, level log.Lvl, options ...rpc.Option) error
	GetVMConfig(ctx context.Context, options ...rpc.Option) (*Config, error)
}

// Client implementation for interacting with EVM [chain]
type client struct {
	adminRequester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with EVM [chain]
func NewClient(uri, chain string) Client {
	return &client{
		adminRequester: rpc.NewEndpointRequester(fmt.Sprintf("%s/ext/bc/%s/admin", uri, chain)),
	}
}

// NewCChainClient returns a Client for interacting with the C Chain
func NewCChainClient(uri string) Client {
	// TODO: Update for Subnet-EVM compatibility
	return NewClient(uri, "C")
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

// SetLogLevel dynamically sets the log level for the C Chain
func (c *client) SetLogLevel(ctx context.Context, level log.Lvl, options ...rpc.Option) error {
	return c.adminRequester.SendRequest(ctx, "admin.setLogLevel", &SetLogLevelArgs{
		Level: level.String(),
	}, &api.EmptyReply{}, options...)
}

// GetVMConfig returns the current config of the VM
func (c *client) GetVMConfig(ctx context.Context, options ...rpc.Option) (*Config, error) {
	res := &ConfigReply{}
	err := c.adminRequester.SendRequest(ctx, "admin.getVMConfig", struct{}{}, res, options...)
	return res.Config, err
}

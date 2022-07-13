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
	StartCPUProfiler(ctx context.Context) error
	StopCPUProfiler(ctx context.Context) error
	MemoryProfile(ctx context.Context) error
	LockProfile(ctx context.Context) error
	SetLogLevel(ctx context.Context, level log.Lvl) error
	GetVMConfig(ctx context.Context) (*Config, error)
}

// Client implementation for interacting with EVM [chain]
type client struct {
	requester      rpc.EndpointRequester
	adminRequester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with EVM [chain]
func NewClient(uri, chain string) Client {
	return &client{
		requester:      rpc.NewEndpointRequester(fmt.Sprintf("%s/ext/bc/%s/avax", uri, chain), "avax"),
		adminRequester: rpc.NewEndpointRequester(fmt.Sprintf("%s/ext/bc/%s/admin", uri, chain), "admin"),
	}
}

// NewCChainClient returns a Client for interacting with the C Chain
func NewCChainClient(uri string) Client {
	// TODO: Update for Subnet-EVM compatibility
	return NewClient(uri, "C")
}

func (c *client) StartCPUProfiler(ctx context.Context) error {
	err := c.adminRequester.SendRequest(ctx, "startCPUProfiler", struct{}{}, &api.EmptyReply{})
	return err
}

func (c *client) StopCPUProfiler(ctx context.Context) error {
	err := c.adminRequester.SendRequest(ctx, "stopCPUProfiler", struct{}{}, &api.EmptyReply{})
	return err
}

func (c *client) MemoryProfile(ctx context.Context) error {
	err := c.adminRequester.SendRequest(ctx, "memoryProfile", struct{}{}, &api.EmptyReply{})
	return err
}

func (c *client) LockProfile(ctx context.Context) error {
	err := c.adminRequester.SendRequest(ctx, "lockProfile", struct{}{}, &api.EmptyReply{})
	return err
}

// SetLogLevel dynamically sets the log level for the C Chain
func (c *client) SetLogLevel(ctx context.Context, level log.Lvl) error {
	err := c.adminRequester.SendRequest(ctx, "setLogLevel", &SetLogLevelArgs{
		Level: level.String(),
	}, &api.EmptyReply{})
	return err
}

// GetVMConfig returns the current config of the VM
func (c *client) GetVMConfig(ctx context.Context) (*Config, error) {
	res := &ConfigReply{}
	err := c.adminRequester.SendRequest(ctx, "getVMConfig", struct{}{}, res)
	return res.Config, err
}

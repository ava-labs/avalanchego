// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ Client = (*client)(nil)

// Client interface for the Avalanche Platform Info API Endpoint
type Client interface {
	StartCPUProfiler() (bool, error)
	StopCPUProfiler() (bool, error)
	MemoryProfile() (bool, error)
	LockProfile() (bool, error)
	Alias(string, string) (bool, error)
	AliasChain(string, string) (bool, error)
	GetChainAliases(string) ([]string, error)
	Stacktrace() (bool, error)
}

// Client implementation for the Avalanche Platform Info API Endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a new Info API Client
func NewClient(uri string, requestTimeout time.Duration) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/admin", "admin", requestTimeout),
	}
}

func (c *client) StartCPUProfiler() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("startCPUProfiler", struct{}{}, res)
	return res.Success, err
}

func (c *client) StopCPUProfiler() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("stopCPUProfiler", struct{}{}, res)
	return res.Success, err
}

func (c *client) MemoryProfile() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("memoryProfile", struct{}{}, res)
	return res.Success, err
}

func (c *client) LockProfile() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("lockProfile", struct{}{}, res)
	return res.Success, err
}

func (c *client) Alias(endpoint, alias string) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("alias", &AliasArgs{
		Endpoint: endpoint,
		Alias:    alias,
	}, res)
	return res.Success, err
}

func (c *client) AliasChain(chain, alias string) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("aliasChain", &AliasChainArgs{
		Chain: chain,
		Alias: alias,
	}, res)
	return res.Success, err
}

func (c *client) GetChainAliases(chain string) ([]string, error) {
	res := &GetChainAliasesReply{}
	err := c.requester.SendRequest("getChainAliases", &GetChainAliasesArgs{
		Chain: chain,
	}, res)
	return res.Aliases, err
}

func (c *client) Stacktrace() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("stacktrace", struct{}{}, res)
	return res.Success, err
}

// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Client for the Avalanche Platform Info API Endpoint
type Client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a new Info API Client
func NewClient(uri string, requestTimeout time.Duration) *Client {
	return &Client{
		requester: rpc.NewEndpointRequester(uri, "/ext/admin", "admin", requestTimeout),
	}
}

// StartCPUProfiler ...
func (c *Client) StartCPUProfiler() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("startCPUProfiler", struct{}{}, res)
	if err != nil {
		return false, err
	}
	return res.Success, nil
}

// StopCPUProfiler ...
func (c *Client) StopCPUProfiler() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("stopCPUProfiler", struct{}{}, res)
	if err != nil {
		return false, err
	}
	return res.Success, nil
}

// MemoryProfile ...
func (c *Client) MemoryProfile() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("memoryProfile", struct{}{}, res)
	if err != nil {
		return false, err
	}
	return res.Success, nil
}

// LockProfile ...
func (c *Client) LockProfile() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("memoryProfile", struct{}{}, res)
	if err != nil {
		return false, err
	}
	return res.Success, nil
}

// Alias ...
func (c *Client) Alias(endpoint, alias string) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("alias", &AliasArgs{
		Endpoint: endpoint,
		Alias:    alias,
	}, res)
	if err != nil {
		return false, err
	}
	return res.Success, nil
}

// AliasChain ...
func (c *Client) AliasChain(chain, alias string) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("aliasChain", &AliasChainArgs{
		Chain: chain,
		Alias: alias,
	}, res)
	if err != nil {
		return false, err
	}
	return res.Success, nil
}

// Stacktrace ...
func (c *Client) Stacktrace() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.requester.SendRequest("stacktrace", struct{}{}, res)
	if err != nil {
		return false, err
	}
	return res.Success, nil
}

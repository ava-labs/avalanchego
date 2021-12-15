// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ethereum/go-ethereum/log"
)

// Client ...
type Client struct {
	adminRequester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with EVM [chain]
func NewClient(uri, chain string, requestTimeout time.Duration) *Client {
	return &Client{
		adminRequester: rpc.NewEndpointRequester(uri, fmt.Sprintf("/ext/bc/%s/admin", chain), "admin", requestTimeout),
	}
}

// NewCChainClient returns a Client for interacting with the C Chain
func NewCChainClient(uri string, requestTimeout time.Duration) *Client {
	return NewClient(uri, "C", requestTimeout)
}

func (c *Client) StartCPUProfiler() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.adminRequester.SendRequest("startCPUProfiler", struct{}{}, res)
	return res.Success, err
}

func (c *Client) StopCPUProfiler() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.adminRequester.SendRequest("stopCPUProfiler", struct{}{}, res)
	return res.Success, err
}

func (c *Client) MemoryProfile() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.adminRequester.SendRequest("memoryProfile", struct{}{}, res)
	return res.Success, err
}

func (c *Client) LockProfile() (bool, error) {
	res := &api.SuccessResponse{}
	err := c.adminRequester.SendRequest("lockProfile", struct{}{}, res)
	return res.Success, err
}

// SetLogLevel dynamically sets the log level for the C Chain
func (c *Client) SetLogLevel(level log.Lvl) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.adminRequester.SendRequest("setLogLevel", &SetLogLevelArgs{
		Level: level.String(),
	}, res)
	return res.Success, err
}

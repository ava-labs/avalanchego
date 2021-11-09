// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ Client = &client{}

// Client interface for an Info API Client
type Client interface {
	GetNodeVersion() (*GetNodeVersionReply, error)
	GetNodeID() (string, error)
	GetNodeIP() (string, error)
	GetNetworkID() (uint32, error)
	GetNetworkName() (string, error)
	GetBlockchainID(alias string) (ids.ID, error)
	Peers() ([]network.PeerID, error)
	IsBootstrapped(string) (bool, error)
	GetTxFee() (*GetTxFeeResponse, error)
}

// Client implementation for an Info API Client
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a new Info API Client
func NewClient(uri string, requestTimeout time.Duration) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/info", "info", requestTimeout),
	}
}

func (c *client) GetNodeVersion() (*GetNodeVersionReply, error) {
	res := &GetNodeVersionReply{}
	err := c.requester.SendRequest("getNodeVersion", struct{}{}, res)
	return res, err
}

func (c *client) GetNodeID() (string, error) {
	res := &GetNodeIDReply{}
	err := c.requester.SendRequest("getNodeID", struct{}{}, res)
	return res.NodeID, err
}

func (c *client) GetNodeIP() (string, error) {
	res := &GetNodeIPReply{}
	err := c.requester.SendRequest("getNodeIP", struct{}{}, res)
	return res.IP, err
}

func (c *client) GetNetworkID() (uint32, error) {
	res := &GetNetworkIDReply{}
	err := c.requester.SendRequest("getNetworkID", struct{}{}, res)
	return uint32(res.NetworkID), err
}

func (c *client) GetNetworkName() (string, error) {
	res := &GetNetworkNameReply{}
	err := c.requester.SendRequest("getNetworkName", struct{}{}, res)
	return res.NetworkName, err
}

func (c *client) GetBlockchainID(alias string) (ids.ID, error) {
	res := &GetBlockchainIDReply{}
	err := c.requester.SendRequest("getBlockchainID", &GetBlockchainIDArgs{
		Alias: alias,
	}, res)
	return res.BlockchainID, err
}

func (c *client) Peers() ([]network.PeerID, error) {
	res := &PeersReply{}
	err := c.requester.SendRequest("peers", struct{}{}, res)
	return res.Peers, err
}

func (c *client) IsBootstrapped(chain string) (bool, error) {
	res := &IsBootstrappedResponse{}
	err := c.requester.SendRequest("isBootstrapped", &IsBootstrappedArgs{
		Chain: chain,
	}, res)
	return res.IsBootstrapped, err
}

func (c *client) GetTxFee() (*GetTxFeeResponse, error) {
	res := &GetTxFeeResponse{}
	err := c.requester.SendRequest("getTxFee", struct{}{}, res)
	return res, err
}

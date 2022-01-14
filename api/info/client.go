// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ Client = &client{}

// Client interface for an Info API Client
type Client interface {
	GetNodeVersion(context.Context) (*GetNodeVersionReply, error)
	GetNodeID(context.Context) (string, error)
	GetNodeIP(context.Context) (string, error)
	GetNetworkID(context.Context) (uint32, error)
	GetNetworkName(context.Context) (string, error)
	GetBlockchainID(context.Context, string) (ids.ID, error)
	Peers(context.Context) ([]network.PeerInfo, error)
	IsBootstrapped(context.Context, string) (bool, error)
	GetTxFee(context.Context) (*GetTxFeeResponse, error)
	Uptime(context.Context) (*UptimeResponse, error)
}

// Client implementation for an Info API Client
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a new Info API Client
func NewClient(uri string) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/info", "info"),
	}
}

func (c *client) GetNodeVersion(ctx context.Context) (*GetNodeVersionReply, error) {
	res := &GetNodeVersionReply{}
	err := c.requester.SendRequest(ctx, "getNodeVersion", struct{}{}, res)
	return res, err
}

func (c *client) GetNodeID(ctx context.Context) (string, error) {
	res := &GetNodeIDReply{}
	err := c.requester.SendRequest(ctx, "getNodeID", struct{}{}, res)
	return res.NodeID, err
}

func (c *client) GetNodeIP(ctx context.Context) (string, error) {
	res := &GetNodeIPReply{}
	err := c.requester.SendRequest(ctx, "getNodeIP", struct{}{}, res)
	return res.IP, err
}

func (c *client) GetNetworkID(ctx context.Context) (uint32, error) {
	res := &GetNetworkIDReply{}
	err := c.requester.SendRequest(ctx, "getNetworkID", struct{}{}, res)
	return uint32(res.NetworkID), err
}

func (c *client) GetNetworkName(ctx context.Context) (string, error) {
	res := &GetNetworkNameReply{}
	err := c.requester.SendRequest(ctx, "getNetworkName", struct{}{}, res)
	return res.NetworkName, err
}

func (c *client) GetBlockchainID(ctx context.Context, alias string) (ids.ID, error) {
	res := &GetBlockchainIDReply{}
	err := c.requester.SendRequest(ctx, "getBlockchainID", &GetBlockchainIDArgs{
		Alias: alias,
	}, res)
	return res.BlockchainID, err
}

func (c *client) Peers(ctx context.Context) ([]network.PeerInfo, error) {
	res := &PeersReply{}
	err := c.requester.SendRequest(ctx, "peers", struct{}{}, res)
	return res.Peers, err
}

func (c *client) IsBootstrapped(ctx context.Context, chainID string) (bool, error) {
	res := &IsBootstrappedResponse{}
	err := c.requester.SendRequest(ctx, "isBootstrapped", &IsBootstrappedArgs{
		Chain: chainID,
	}, res)
	return res.IsBootstrapped, err
}

func (c *client) GetTxFee(ctx context.Context) (*GetTxFeeResponse, error) {
	res := &GetTxFeeResponse{}
	err := c.requester.SendRequest(ctx, "getTxFee", struct{}{}, res)
	return res, err
}

func (c *client) Uptime(ctx context.Context) (*UptimeResponse, error) {
	res := &UptimeResponse{}
	err := c.requester.SendRequest(ctx, "uptime", struct{}{}, res)
	return res, err
}

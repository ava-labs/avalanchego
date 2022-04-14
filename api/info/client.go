// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"context"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/rpc"
)

// Interface compliance
var _ Client = &client{}

// Client interface for an Info API Client
type Client interface {
	GetNodeVersion(context.Context, ...rpc.Option) (*GetNodeVersionReply, error)
	GetNodeID(context.Context, ...rpc.Option) (string, error)
	GetNodeIP(context.Context, ...rpc.Option) (string, error)
	GetNetworkID(context.Context, ...rpc.Option) (uint32, error)
	GetNetworkName(context.Context, ...rpc.Option) (string, error)
	GetBlockchainID(context.Context, string, ...rpc.Option) (ids.ID, error)
	Peers(context.Context, ...rpc.Option) ([]Peer, error)
	IsBootstrapped(context.Context, string, ...rpc.Option) (bool, error)
	GetTxFee(context.Context, ...rpc.Option) (*GetTxFeeResponse, error)
	Uptime(context.Context, ...rpc.Option) (*UptimeResponse, error)
	GetVMs(context.Context, ...rpc.Option) (map[ids.ID][]string, error)
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

func (c *client) GetNodeVersion(ctx context.Context, options ...rpc.Option) (*GetNodeVersionReply, error) {
	res := &GetNodeVersionReply{}
	err := c.requester.SendRequest(ctx, "getNodeVersion", struct{}{}, res, options...)
	return res, err
}

func (c *client) GetNodeID(ctx context.Context, options ...rpc.Option) (string, error) {
	res := &GetNodeIDReply{}
	err := c.requester.SendRequest(ctx, "getNodeID", struct{}{}, res, options...)
	return res.NodeID, err
}

func (c *client) GetNodeIP(ctx context.Context, options ...rpc.Option) (string, error) {
	res := &GetNodeIPReply{}
	err := c.requester.SendRequest(ctx, "getNodeIP", struct{}{}, res, options...)
	return res.IP, err
}

func (c *client) GetNetworkID(ctx context.Context, options ...rpc.Option) (uint32, error) {
	res := &GetNetworkIDReply{}
	err := c.requester.SendRequest(ctx, "getNetworkID", struct{}{}, res, options...)
	return uint32(res.NetworkID), err
}

func (c *client) GetNetworkName(ctx context.Context, options ...rpc.Option) (string, error) {
	res := &GetNetworkNameReply{}
	err := c.requester.SendRequest(ctx, "getNetworkName", struct{}{}, res, options...)
	return res.NetworkName, err
}

func (c *client) GetBlockchainID(ctx context.Context, alias string, options ...rpc.Option) (ids.ID, error) {
	res := &GetBlockchainIDReply{}
	err := c.requester.SendRequest(ctx, "getBlockchainID", &GetBlockchainIDArgs{
		Alias: alias,
	}, res, options...)
	return res.BlockchainID, err
}

func (c *client) Peers(ctx context.Context, options ...rpc.Option) ([]Peer, error) {
	res := &PeersReply{}
	err := c.requester.SendRequest(ctx, "peers", struct{}{}, res, options...)
	return res.Peers, err
}

func (c *client) IsBootstrapped(ctx context.Context, chainID string, options ...rpc.Option) (bool, error) {
	res := &IsBootstrappedResponse{}
	err := c.requester.SendRequest(ctx, "isBootstrapped", &IsBootstrappedArgs{
		Chain: chainID,
	}, res, options...)
	return res.IsBootstrapped, err
}

func (c *client) GetTxFee(ctx context.Context, options ...rpc.Option) (*GetTxFeeResponse, error) {
	res := &GetTxFeeResponse{}
	err := c.requester.SendRequest(ctx, "getTxFee", struct{}{}, res, options...)
	return res, err
}

func (c *client) Uptime(ctx context.Context, options ...rpc.Option) (*UptimeResponse, error) {
	res := &UptimeResponse{}
	err := c.requester.SendRequest(ctx, "uptime", struct{}{}, res, options...)
	return res, err
}

func (c *client) GetVMs(ctx context.Context, options ...rpc.Option) (map[ids.ID][]string, error) {
	res := &GetVMsReply{}
	err := c.requester.SendRequest(ctx, "getVMs", struct{}{}, res, options...)
	return res.VMs, err
}

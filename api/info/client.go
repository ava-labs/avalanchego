// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"context"
	"net/netip"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
)

var _ Client = (*client)(nil)

// Client interface for an Info API Client.
// See also AwaitBootstrapped.
type Client interface {
	GetNodeVersion(context.Context, ...rpc.Option) (*GetNodeVersionReply, error)
	GetNodeID(context.Context, ...rpc.Option) (ids.NodeID, *signer.ProofOfPossession, error)
	GetNodeIP(context.Context, ...rpc.Option) (netip.AddrPort, error)
	GetNetworkID(context.Context, ...rpc.Option) (uint32, error)
	GetNetworkName(context.Context, ...rpc.Option) (string, error)
	GetBlockchainID(context.Context, string, ...rpc.Option) (ids.ID, error)
	Peers(context.Context, ...rpc.Option) ([]Peer, error)
	IsBootstrapped(context.Context, string, ...rpc.Option) (bool, error)
	GetTxFee(context.Context, ...rpc.Option) (*GetTxFeeResponse, error)
	Upgrades(context.Context, ...rpc.Option) (*upgrade.Config, error)
	Uptime(context.Context, ...rpc.Option) (*UptimeResponse, error)
	GetVMs(context.Context, ...rpc.Option) (map[ids.ID][]string, error)
}

// Client implementation for an Info API Client
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a new Info API Client
func NewClient(uri string) Client {
	return &client{requester: rpc.NewEndpointRequester(
		uri + "/ext/info",
	)}
}

func (c *client) GetNodeVersion(ctx context.Context, options ...rpc.Option) (*GetNodeVersionReply, error) {
	res := &GetNodeVersionReply{}
	err := c.requester.SendRequest(ctx, "info.getNodeVersion", struct{}{}, res, options...)
	return res, err
}

func (c *client) GetNodeID(ctx context.Context, options ...rpc.Option) (ids.NodeID, *signer.ProofOfPossession, error) {
	res := &GetNodeIDReply{}
	err := c.requester.SendRequest(ctx, "info.getNodeID", struct{}{}, res, options...)
	return res.NodeID, res.NodePOP, err
}

func (c *client) GetNodeIP(ctx context.Context, options ...rpc.Option) (netip.AddrPort, error) {
	res := &GetNodeIPReply{}
	err := c.requester.SendRequest(ctx, "info.getNodeIP", struct{}{}, res, options...)
	return res.IP, err
}

func (c *client) GetNetworkID(ctx context.Context, options ...rpc.Option) (uint32, error) {
	res := &GetNetworkIDReply{}
	err := c.requester.SendRequest(ctx, "info.getNetworkID", struct{}{}, res, options...)
	return uint32(res.NetworkID), err
}

func (c *client) GetNetworkName(ctx context.Context, options ...rpc.Option) (string, error) {
	res := &GetNetworkNameReply{}
	err := c.requester.SendRequest(ctx, "info.getNetworkName", struct{}{}, res, options...)
	return res.NetworkName, err
}

func (c *client) GetBlockchainID(ctx context.Context, alias string, options ...rpc.Option) (ids.ID, error) {
	res := &GetBlockchainIDReply{}
	err := c.requester.SendRequest(ctx, "info.getBlockchainID", &GetBlockchainIDArgs{
		Alias: alias,
	}, res, options...)
	return res.BlockchainID, err
}

func (c *client) Peers(ctx context.Context, options ...rpc.Option) ([]Peer, error) {
	res := &PeersReply{}
	err := c.requester.SendRequest(ctx, "info.peers", struct{}{}, res, options...)
	return res.Peers, err
}

func (c *client) IsBootstrapped(ctx context.Context, chainID string, options ...rpc.Option) (bool, error) {
	res := &IsBootstrappedResponse{}
	err := c.requester.SendRequest(ctx, "info.isBootstrapped", &IsBootstrappedArgs{
		Chain: chainID,
	}, res, options...)
	return res.IsBootstrapped, err
}

func (c *client) GetTxFee(ctx context.Context, options ...rpc.Option) (*GetTxFeeResponse, error) {
	res := &GetTxFeeResponse{}
	err := c.requester.SendRequest(ctx, "info.getTxFee", struct{}{}, res, options...)
	return res, err
}

func (c *client) Upgrades(ctx context.Context, options ...rpc.Option) (*upgrade.Config, error) {
	res := &upgrade.Config{}
	err := c.requester.SendRequest(ctx, "info.upgrades", struct{}{}, res, options...)
	return res, err
}

func (c *client) Uptime(ctx context.Context, options ...rpc.Option) (*UptimeResponse, error) {
	res := &UptimeResponse{}
	err := c.requester.SendRequest(ctx, "info.uptime", struct{}{}, res, options...)
	return res, err
}

func (c *client) GetVMs(ctx context.Context, options ...rpc.Option) (map[ids.ID][]string, error) {
	res := &GetVMsReply{}
	err := c.requester.SendRequest(ctx, "info.getVMs", struct{}{}, res, options...)
	return res.VMs, err
}

// AwaitBootstrapped polls the node every [freq] to check if [chainID] has
// finished bootstrapping. Returns true once [chainID] reports that it has
// finished bootstrapping.
// Only returns an error if [ctx] returns an error.
func AwaitBootstrapped(ctx context.Context, c Client, chainID string, freq time.Duration, options ...rpc.Option) (bool, error) {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	for {
		res, err := c.IsBootstrapped(ctx, chainID, options...)
		if err == nil && res {
			return true, nil
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

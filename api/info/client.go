// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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

type Client struct {
	Requester rpc.EndpointRequester
}

func NewClient(uri string) *Client {
	return &Client{Requester: rpc.NewEndpointRequester(
		uri + "/ext/info",
	)}
}

func (c *Client) GetNodeVersion(ctx context.Context, options ...rpc.Option) (*GetNodeVersionReply, error) {
	res := &GetNodeVersionReply{}
	err := c.Requester.SendRequest(ctx, "info.getNodeVersion", struct{}{}, res, options...)
	return res, err
}

func (c *Client) GetNodeID(ctx context.Context, options ...rpc.Option) (ids.NodeID, *signer.ProofOfPossession, error) {
	res := &GetNodeIDReply{}
	err := c.Requester.SendRequest(ctx, "info.getNodeID", struct{}{}, res, options...)
	return res.NodeID, res.NodePOP, err
}

func (c *Client) GetNodeIP(ctx context.Context, options ...rpc.Option) (netip.AddrPort, error) {
	res := &GetNodeIPReply{}
	err := c.Requester.SendRequest(ctx, "info.getNodeIP", struct{}{}, res, options...)
	return res.IP, err
}

func (c *Client) GetNetworkID(ctx context.Context, options ...rpc.Option) (uint32, error) {
	res := &GetNetworkIDReply{}
	err := c.Requester.SendRequest(ctx, "info.getNetworkID", struct{}{}, res, options...)
	return uint32(res.NetworkID), err
}

func (c *Client) GetNetworkName(ctx context.Context, options ...rpc.Option) (string, error) {
	res := &GetNetworkNameReply{}
	err := c.Requester.SendRequest(ctx, "info.getNetworkName", struct{}{}, res, options...)
	return res.NetworkName, err
}

func (c *Client) GetBlockchainID(ctx context.Context, alias string, options ...rpc.Option) (ids.ID, error) {
	res := &GetBlockchainIDReply{}
	err := c.Requester.SendRequest(ctx, "info.getBlockchainID", &GetBlockchainIDArgs{
		Alias: alias,
	}, res, options...)
	return res.BlockchainID, err
}

func (c *Client) Peers(ctx context.Context, nodeIDs []ids.NodeID, options ...rpc.Option) ([]Peer, error) {
	res := &PeersReply{}
	err := c.Requester.SendRequest(ctx, "info.peers", &PeersArgs{
		NodeIDs: nodeIDs,
	}, res, options...)
	return res.Peers, err
}

func (c *Client) IsBootstrapped(ctx context.Context, chainID string, options ...rpc.Option) (bool, error) {
	res := &IsBootstrappedResponse{}
	err := c.Requester.SendRequest(ctx, "info.isBootstrapped", &IsBootstrappedArgs{
		Chain: chainID,
	}, res, options...)
	return res.IsBootstrapped, err
}

func (c *Client) Upgrades(ctx context.Context, options ...rpc.Option) (*upgrade.Config, error) {
	res := &upgrade.Config{}
	err := c.Requester.SendRequest(ctx, "info.upgrades", struct{}{}, res, options...)
	return res, err
}

func (c *Client) Uptime(ctx context.Context, options ...rpc.Option) (*UptimeResponse, error) {
	res := &UptimeResponse{}
	err := c.Requester.SendRequest(ctx, "info.uptime", struct{}{}, res, options...)
	return res, err
}

func (c *Client) GetVMs(ctx context.Context, options ...rpc.Option) (map[ids.ID][]string, error) {
	res := &GetVMsReply{}
	err := c.Requester.SendRequest(ctx, "info.getVMs", struct{}{}, res, options...)
	return res.VMs, err
}

// AwaitBootstrapped polls the node every [freq] to check if [chainID] has
// finished bootstrapping. Returns true once [chainID] reports that it has
// finished bootstrapping.
// Only returns an error if [ctx] returns an error.
func AwaitBootstrapped(ctx context.Context, c *Client, chainID string, freq time.Duration, options ...rpc.Option) (bool, error) {
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

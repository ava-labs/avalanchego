// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"

	"github.com/MetalBlockchain/metalgo/api/info"
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/snow"
	"github.com/MetalBlockchain/metalgo/utils/constants"
	"github.com/MetalBlockchain/metalgo/utils/logging"
	"github.com/MetalBlockchain/metalgo/vms/avm"
)

const Alias = "C"

type Context struct {
	NetworkID    uint32
	BlockchainID ids.ID
	AVAXAssetID  ids.ID
}

func NewContextFromURI(ctx context.Context, uri string) (*Context, error) {
	infoClient := info.NewClient(uri)
	xChainClient := avm.NewClient(uri, "X")
	return NewContextFromClients(ctx, infoClient, xChainClient)
}

func NewContextFromClients(
	ctx context.Context,
	infoClient *info.Client,
	xChainClient *avm.Client,
) (*Context, error) {
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		return nil, err
	}

	blockchainID, err := infoClient.GetBlockchainID(ctx, Alias)
	if err != nil {
		return nil, err
	}

	avaxAsset, err := xChainClient.GetAssetDescription(ctx, "METAL")
	if err != nil {
		return nil, err
	}

	return &Context{
		NetworkID:    networkID,
		BlockchainID: blockchainID,
		AVAXAssetID:  avaxAsset.AssetID,
	}, nil
}

func newSnowContext(c *Context) (*snow.Context, error) {
	lookup := ids.NewAliaser()
	return &snow.Context{
		NetworkID:   c.NetworkID,
		SubnetID:    constants.PrimaryNetworkID,
		ChainID:     c.BlockchainID,
		CChainID:    c.BlockchainID,
		AVAXAssetID: c.AVAXAssetID,
		Log:         logging.NoLog{},
		BCLookup:    lookup,
	}, lookup.Alias(c.BlockchainID, Alias)
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"context"

	"github.com/MetalBlockchain/metalgo/api/info"
	"github.com/MetalBlockchain/metalgo/vms/avm"
	"github.com/MetalBlockchain/metalgo/wallet/chain/x/builder"
)

func NewContextFromURI(ctx context.Context, uri string) (*builder.Context, error) {
	infoClient := info.NewClient(uri)
	xChainClient := avm.NewClient(uri, builder.Alias)
	return NewContextFromClients(ctx, infoClient, xChainClient)
}

func NewContextFromClients(
	ctx context.Context,
	infoClient *info.Client,
	xChainClient *avm.Client,
) (*builder.Context, error) {
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		return nil, err
	}

	chainID, err := infoClient.GetBlockchainID(ctx, builder.Alias)
	if err != nil {
		return nil, err
	}

	asset, err := xChainClient.GetAssetDescription(ctx, "METAL")
	if err != nil {
		return nil, err
	}

	baseTxFee, createAssetTxFee, err := xChainClient.GetTxFee(ctx)
	if err != nil {
		return nil, err
	}

	return &builder.Context{
		NetworkID:        networkID,
		BlockchainID:     chainID,
		AVAXAssetID:      asset.AssetID,
		BaseTxFee:        baseTxFee,
		CreateAssetTxFee: createAssetTxFee,
	}, nil
}

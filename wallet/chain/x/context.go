// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"context"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/wallet/chain/x/builder"
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

	asset, err := xChainClient.GetAssetDescription(ctx, "AVAX")
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

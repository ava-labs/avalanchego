// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"context"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
)

// gasPriceMultiplier increases the gas price to support multiple transactions
// to be issued.
//
// TODO: Handle this better. Either here or in the mempool.
const gasPriceMultiplier = 2

func NewContextFromURI(ctx context.Context, uri string) (*builder.Context, error) {
	infoClient := info.NewClient(uri)
	xChainClient := avm.NewClient(uri, "X")
	pChainClient := platformvm.NewClient(uri)
	return NewContextFromClients(ctx, infoClient, xChainClient, pChainClient)
}

func NewContextFromClients(
	ctx context.Context,
	infoClient info.Client,
	xChainClient avm.Client,
	pChainClient platformvm.Client,
) (*builder.Context, error) {
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		return nil, err
	}

	asset, err := xChainClient.GetAssetDescription(ctx, "AVAX")
	if err != nil {
		return nil, err
	}

	dynamicFeeConfig, err := pChainClient.GetFeeConfig(ctx)
	if err != nil {
		return nil, err
	}

	// TODO: After Etna is activated, assume the gas price is always non-zero.
	if dynamicFeeConfig.MinPrice != 0 {
		_, gasPrice, _, err := pChainClient.GetFeeState(ctx)
		if err != nil {
			return nil, err
		}

		return &builder.Context{
			NetworkID:         networkID,
			AVAXAssetID:       asset.AssetID,
			ComplexityWeights: dynamicFeeConfig.Weights,
			GasPrice:          gasPriceMultiplier * gasPrice,
		}, nil
	}

	staticFeeConfig, err := infoClient.GetTxFee(ctx)
	if err != nil {
		return nil, err
	}

	return &builder.Context{
		NetworkID:   networkID,
		AVAXAssetID: asset.AssetID,
		StaticFeeConfig: fee.StaticConfig{
			TxFee:                         uint64(staticFeeConfig.TxFee),
			CreateSubnetTxFee:             uint64(staticFeeConfig.CreateSubnetTxFee),
			TransformSubnetTxFee:          uint64(staticFeeConfig.TransformSubnetTxFee),
			CreateBlockchainTxFee:         uint64(staticFeeConfig.CreateBlockchainTxFee),
			AddPrimaryNetworkValidatorFee: uint64(staticFeeConfig.AddPrimaryNetworkValidatorFee),
			AddPrimaryNetworkDelegatorFee: uint64(staticFeeConfig.AddPrimaryNetworkDelegatorFee),
			AddSubnetValidatorFee:         uint64(staticFeeConfig.AddSubnetValidatorFee),
			AddSubnetDelegatorFee:         uint64(staticFeeConfig.AddSubnetDelegatorFee),
		},
	}, nil
}

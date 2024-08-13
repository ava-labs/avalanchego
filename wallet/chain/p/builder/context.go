// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/avm"

	feecomponent "github.com/ava-labs/avalanchego/vms/components/fee"
	txfee "github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

const Alias = "P"

type Context struct {
	NetworkID         uint32
	AVAXAssetID       ids.ID
	StaticFeeConfig   txfee.StaticConfig
	ComplexityWeights feecomponent.Dimensions
	GasPrice          feecomponent.GasPrice
}

func NewContextFromURI(ctx context.Context, uri string) (*Context, error) {
	infoClient := info.NewClient(uri)
	xChainClient := avm.NewClient(uri, "X")
	return NewContextFromClients(ctx, infoClient, xChainClient)
}

func NewContextFromClients(
	ctx context.Context,
	infoClient info.Client,
	xChainClient avm.Client,
) (*Context, error) {
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		return nil, err
	}

	asset, err := xChainClient.GetAssetDescription(ctx, "AVAX")
	if err != nil {
		return nil, err
	}

	txFees, err := infoClient.GetTxFee(ctx)
	if err != nil {
		return nil, err
	}

	return &Context{
		NetworkID:   networkID,
		AVAXAssetID: asset.AssetID,
		StaticFeeConfig: txfee.StaticConfig{
			TxFee:                         uint64(txFees.TxFee),
			CreateSubnetTxFee:             uint64(txFees.CreateSubnetTxFee),
			TransformSubnetTxFee:          uint64(txFees.TransformSubnetTxFee),
			CreateBlockchainTxFee:         uint64(txFees.CreateBlockchainTxFee),
			AddPrimaryNetworkValidatorFee: uint64(txFees.AddPrimaryNetworkValidatorFee),
			AddPrimaryNetworkDelegatorFee: uint64(txFees.AddPrimaryNetworkDelegatorFee),
			AddSubnetValidatorFee:         uint64(txFees.AddSubnetValidatorFee),
			AddSubnetDelegatorFee:         uint64(txFees.AddSubnetDelegatorFee),
		},

		// TODO: Populate these fields once they are exposed by the API
		ComplexityWeights: feecomponent.Dimensions{},
		GasPrice:          0,
	}, nil
}

func NewSnowContext(networkID uint32, avaxAssetID ids.ID) (*snow.Context, error) {
	lookup := ids.NewAliaser()
	return &snow.Context{
		NetworkID:   networkID,
		SubnetID:    constants.PrimaryNetworkID,
		ChainID:     constants.PlatformChainID,
		AVAXAssetID: avaxAssetID,
		Log:         logging.NoLog{},
		BCLookup:    lookup,
	}, lookup.Alias(constants.PlatformChainID, Alias)
}

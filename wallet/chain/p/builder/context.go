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
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

const Alias = "P"

type Context struct {
	NetworkID       uint32
	AVAXAssetID     ids.ID
	StaticFeeConfig fee.StaticConfig
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
		StaticFeeConfig: fee.StaticConfig{
			TxFee:                         uint64(txFees.TxFee),
			CreateSubnetTxFee:             uint64(txFees.CreateSubnetTxFee),
			TransformSubnetTxFee:          uint64(txFees.TransformSubnetTxFee),
			CreateBlockchainTxFee:         uint64(txFees.CreateBlockchainTxFee),
			AddPrimaryNetworkValidatorFee: uint64(txFees.AddPrimaryNetworkValidatorFee),
			AddPrimaryNetworkDelegatorFee: uint64(txFees.AddPrimaryNetworkDelegatorFee),
			AddSubnetValidatorFee:         uint64(txFees.AddSubnetValidatorFee),
			AddSubnetDelegatorFee:         uint64(txFees.AddSubnetDelegatorFee),
		},
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

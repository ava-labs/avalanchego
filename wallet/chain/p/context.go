// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/backends"

	stdcontext "context"
)

func NewContextFromURI(ctx stdcontext.Context, uri string) (backends.Context, error) {
	infoClient := info.NewClient(uri)
	xChainClient := avm.NewClient(uri, "X")
	return NewContextFromClients(ctx, infoClient, xChainClient)
}

func NewContextFromClients(
	ctx stdcontext.Context,
	infoClient info.Client,
	xChainClient avm.Client,
) (backends.Context, error) {
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

	return backends.NewContext(
		networkID,
		asset.AssetID,
		uint64(txFees.TxFee),
		uint64(txFees.CreateSubnetTxFee),
		uint64(txFees.TransformSubnetTxFee),
		uint64(txFees.CreateBlockchainTxFee),
		uint64(txFees.AddPrimaryNetworkValidatorFee),
		uint64(txFees.AddPrimaryNetworkDelegatorFee),
		uint64(txFees.AddSubnetValidatorFee),
		uint64(txFees.AddSubnetDelegatorFee),
	), nil
}

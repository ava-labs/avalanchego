// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	stdcontext "context"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
)

var _ Context = &context{}

type Context interface {
	NetworkID() uint32
	AVAXAssetID() ids.ID
	TxFees() config.TxFees
}

type context struct {
	networkID   uint32
	avaxAssetID ids.ID
	txFees      config.TxFees
}

func NewContextFromURI(ctx stdcontext.Context, uri string) (Context, error) {
	infoClient := info.NewClient(uri)
	xChainClient := avm.NewClient(uri, "X")
	return NewContextFromClients(ctx, infoClient, xChainClient)
}

func NewContextFromClients(
	ctx stdcontext.Context,
	infoClient info.Client,
	xChainClient avm.Client,
) (Context, error) {
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

	return NewContext(
		networkID,
		asset.AssetID,
		config.TxFees{
			AddPrimaryNetworkValidator: uint64(txFees.PChainAddPrimaryNetworkValidator),
			AddPrimaryNetworkDelegator: uint64(txFees.PChainAddPrimaryNetworkDelegator),
			AddPOASubnetValidator:      uint64(txFees.PChainAddPOASubnetValidator),
			AddPOSSubnetValidator:      uint64(txFees.PChainAddPOSSubnetValidator),
			AddPOSSubnetDelegator:      uint64(txFees.PChainAddPOSSubnetDelegator),
			RemovePOASubnetValidator:   uint64(txFees.PChainRemovePOASubnetValidator),
			CreateSubnet:               uint64(txFees.PChainCreateSubnet),
			CreateChain:                uint64(txFees.PChainCreateChain),
			TransformSubnet:            uint64(txFees.PChainTransformSubnet),
			Import:                     uint64(txFees.PChainImport),
			Export:                     uint64(txFees.PChainExport),
		},
	), nil
}

func NewContext(
	networkID uint32,
	avaxAssetID ids.ID,
	txFees config.TxFees,
) Context {
	return &context{
		networkID:   networkID,
		avaxAssetID: avaxAssetID,
		txFees:      txFees,
	}
}

func (c *context) NetworkID() uint32     { return c.networkID }
func (c *context) AVAXAssetID() ids.ID   { return c.avaxAssetID }
func (c *context) TxFees() config.TxFees { return c.txFees }

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	stdcontext "context"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm"
)

var _ Context = (*context)(nil)

type Context interface {
	NetworkID() uint32
	AVAXAssetID() ids.ID
	BaseTxFee() uint64
	CreateSubnetTxFee() uint64
	TransformSubnetTxFee() uint64
	CreateBlockchainTxFee() uint64
	AddPrimaryNetworkValidatorFee() uint64
	AddPrimaryNetworkDelegatorFee() uint64
	AddSubnetValidatorFee() uint64
	AddSubnetDelegatorFee() uint64
}

type context struct {
	networkID                     uint32
	avaxAssetID                   ids.ID
	baseTxFee                     uint64
	createSubnetTxFee             uint64
	transformSubnetTxFee          uint64
	createBlockchainTxFee         uint64
	addPrimaryNetworkValidatorFee uint64
	addPrimaryNetworkDelegatorFee uint64
	addSubnetValidatorFee         uint64
	addSubnetDelegatorFee         uint64
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

func NewContext(
	networkID uint32,
	avaxAssetID ids.ID,
	baseTxFee uint64,
	createSubnetTxFee uint64,
	transformSubnetTxFee uint64,
	createBlockchainTxFee uint64,
	addPrimaryNetworkValidatorFee uint64,
	addPrimaryNetworkDelegatorFee uint64,
	addSubnetValidatorFee uint64,
	addSubnetDelegatorFee uint64,
) Context {
	return &context{
		networkID:                     networkID,
		avaxAssetID:                   avaxAssetID,
		baseTxFee:                     baseTxFee,
		createSubnetTxFee:             createSubnetTxFee,
		transformSubnetTxFee:          transformSubnetTxFee,
		createBlockchainTxFee:         createBlockchainTxFee,
		addPrimaryNetworkValidatorFee: addPrimaryNetworkValidatorFee,
		addPrimaryNetworkDelegatorFee: addPrimaryNetworkDelegatorFee,
		addSubnetValidatorFee:         addSubnetValidatorFee,
		addSubnetDelegatorFee:         addSubnetDelegatorFee,
	}
}

func (c *context) NetworkID() uint32 {
	return c.networkID
}

func (c *context) AVAXAssetID() ids.ID {
	return c.avaxAssetID
}

func (c *context) BaseTxFee() uint64 {
	return c.baseTxFee
}

func (c *context) CreateSubnetTxFee() uint64 {
	return c.createSubnetTxFee
}

func (c *context) TransformSubnetTxFee() uint64 {
	return c.transformSubnetTxFee
}

func (c *context) CreateBlockchainTxFee() uint64 {
	return c.createBlockchainTxFee
}

func (c *context) AddPrimaryNetworkValidatorFee() uint64 {
	return c.addPrimaryNetworkValidatorFee
}

func (c *context) AddPrimaryNetworkDelegatorFee() uint64 {
	return c.addPrimaryNetworkDelegatorFee
}

func (c *context) AddSubnetValidatorFee() uint64 {
	return c.addSubnetValidatorFee
}

func (c *context) AddSubnetDelegatorFee() uint64 {
	return c.addSubnetDelegatorFee
}

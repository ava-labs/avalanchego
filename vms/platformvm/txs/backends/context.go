// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package backends

import "github.com/ava-labs/avalanchego/ids"

var _ Context = (*builderCtx)(nil)

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

type builderCtx struct {
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
	return &builderCtx{
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

func (c *builderCtx) NetworkID() uint32 {
	return c.networkID
}

func (c *builderCtx) AVAXAssetID() ids.ID {
	return c.avaxAssetID
}

func (c *builderCtx) BaseTxFee() uint64 {
	return c.baseTxFee
}

func (c *builderCtx) CreateSubnetTxFee() uint64 {
	return c.createSubnetTxFee
}

func (c *builderCtx) TransformSubnetTxFee() uint64 {
	return c.transformSubnetTxFee
}

func (c *builderCtx) CreateBlockchainTxFee() uint64 {
	return c.createBlockchainTxFee
}

func (c *builderCtx) AddPrimaryNetworkValidatorFee() uint64 {
	return c.addPrimaryNetworkValidatorFee
}

func (c *builderCtx) AddPrimaryNetworkDelegatorFee() uint64 {
	return c.addPrimaryNetworkDelegatorFee
}

func (c *builderCtx) AddSubnetValidatorFee() uint64 {
	return c.addSubnetValidatorFee
}

func (c *builderCtx) AddSubnetDelegatorFee() uint64 {
	return c.addSubnetDelegatorFee
}

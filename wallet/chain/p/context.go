// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	stdcontext "context"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/avm"
)

var _ Context = &context{}

type Context interface {
	NetworkID() uint32
	HRP() string
	AVAXAssetID() ids.ID
	BaseTxFee() uint64
	CreateSubnetTxFee() uint64
	CreateBlockchainTxFee() uint64
}

type context struct {
	networkID             uint32
	hrp                   string
	avaxAssetID           ids.ID
	baseTxFee             uint64
	createSubnetTxFee     uint64
	createBlockchainTxFee uint64
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
		uint64(txFees.CreateBlockchainTxFee),
	), nil
}

func NewContext(
	networkID uint32,
	avaxAssetID ids.ID,
	baseTxFee uint64,
	createSubnetTxFee uint64,
	createBlockchainTxFee uint64,
) Context {
	return &context{
		networkID:             networkID,
		hrp:                   constants.GetHRP(networkID),
		avaxAssetID:           avaxAssetID,
		baseTxFee:             baseTxFee,
		createSubnetTxFee:     createSubnetTxFee,
		createBlockchainTxFee: createBlockchainTxFee,
	}
}

func (c *context) NetworkID() uint32             { return c.networkID }
func (c *context) HRP() string                   { return c.hrp }
func (c *context) AVAXAssetID() ids.ID           { return c.avaxAssetID }
func (c *context) BaseTxFee() uint64             { return c.baseTxFee }
func (c *context) CreateSubnetTxFee() uint64     { return c.createSubnetTxFee }
func (c *context) CreateBlockchainTxFee() uint64 { return c.createBlockchainTxFee }

// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	stdcontext "context"

	"github.com/chain4travel/caminogo/api/info"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/constants"
	"github.com/chain4travel/caminogo/vms/avm"
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

	asset, err := xChainClient.GetAssetDescription(ctx, constants.TokenSymbol(networkID))
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

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	stdcontext "context"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/avm/config"
)

var _ Context = &context{}

type Context interface {
	NetworkID() uint32
	BlockchainID() ids.ID
	AVAXAssetID() ids.ID
	TxFees() config.TxFees
}

type context struct {
	networkID    uint32
	blockchainID ids.ID
	avaxAssetID  ids.ID
	txFees       config.TxFees
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

	chainID, err := infoClient.GetBlockchainID(ctx, "X")
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
		chainID,
		asset.AssetID,
		config.TxFees{
			Base:        uint64(txFees.XChainBase),
			CreateAsset: uint64(txFees.XChainCreateAsset),
			Operation:   uint64(txFees.XChainOperation),
			Import:      uint64(txFees.XChainImport),
			Export:      uint64(txFees.XChainExport),
		},
	), nil
}

func NewContext(
	networkID uint32,
	blockchainID ids.ID,
	avaxAssetID ids.ID,
	txFees config.TxFees,
) Context {
	return &context{
		networkID:    networkID,
		blockchainID: blockchainID,
		avaxAssetID:  avaxAssetID,
		txFees:       txFees,
	}
}

func (c *context) NetworkID() uint32     { return c.networkID }
func (c *context) BlockchainID() ids.ID  { return c.blockchainID }
func (c *context) AVAXAssetID() ids.ID   { return c.avaxAssetID }
func (c *context) TxFees() config.TxFees { return c.txFees }

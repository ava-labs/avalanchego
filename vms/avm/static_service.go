// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ avax.TransferableIn  = (*secp256k1fx.TransferInput)(nil)
	_ verify.State         = (*secp256k1fx.MintOutput)(nil)
	_ avax.TransferableOut = (*secp256k1fx.TransferOutput)(nil)
	_ fxs.FxOperation      = (*secp256k1fx.MintOperation)(nil)
	_ verify.Verifiable    = (*secp256k1fx.Credential)(nil)

	_ verify.State      = (*nftfx.MintOutput)(nil)
	_ verify.State      = (*nftfx.TransferOutput)(nil)
	_ fxs.FxOperation   = (*nftfx.MintOperation)(nil)
	_ fxs.FxOperation   = (*nftfx.TransferOperation)(nil)
	_ verify.Verifiable = (*nftfx.Credential)(nil)

	_ verify.State      = (*propertyfx.MintOutput)(nil)
	_ verify.State      = (*propertyfx.OwnedOutput)(nil)
	_ fxs.FxOperation   = (*propertyfx.MintOperation)(nil)
	_ fxs.FxOperation   = (*propertyfx.BurnOperation)(nil)
	_ verify.Verifiable = (*propertyfx.Credential)(nil)
)

// AssetInitialState describes the initial state of an asset
type AssetInitialState struct {
	FixedCap    []Holder
	VariableCap []Owners
}

// AssetDefinition describes a genesis asset and its initial state
type AssetDefinition struct {
	Name         string
	Symbol       string
	Denomination uint8
	InitialState AssetInitialState
	Memo         []byte
}

// Holder describes how much asset is owned by an address
type Holder struct {
	Amount  uint64
	Address string
}

// Owners describes who can perform an action
type Owners struct {
	Threshold uint32
	Minters   []string
}

// NewGenesisBytes creates a new genesis bytes from genesis data
func NewGenesisBytes(
	networkID uint32,
	genesisData map[string]AssetDefinition,
) ([]byte, error) {
	parser, err := txs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
			&nftfx.Fx{},
			&propertyfx.Fx{},
		},
	)
	if err != nil {
		return nil, err
	}

	g := Genesis{}
	genesisCodec := parser.GenesisCodec()
	for assetAlias, assetDefinition := range genesisData {
		if err != nil {
			return nil, fmt.Errorf("problem formatting asset definition memo due to: %w", err)
		}
		asset := GenesisAsset{
			Alias: assetAlias,
			CreateAssetTx: txs.CreateAssetTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    networkID,
					BlockchainID: ids.Empty,
					Memo:         assetDefinition.Memo,
				}},
				Name:         assetDefinition.Name,
				Symbol:       assetDefinition.Symbol,
				Denomination: assetDefinition.Denomination,
			},
		}

		initialState := &txs.InitialState{
			FxIndex: 0, // TODO: Should lookup secp256k1fx FxID
		}
		if len(assetDefinition.InitialState.FixedCap) > 0 {
			for _, holder := range assetDefinition.InitialState.FixedCap {
				_, addrbuff, err := address.ParseBech32(holder.Address)
				if err != nil {
					return nil, fmt.Errorf("problem parsing holder address: %w", err)
				}
				addr, err := ids.ToShortID(addrbuff)
				if err != nil {
					return nil, fmt.Errorf("problem parsing holder address: %w", err)
				}
				initialState.Outs = append(initialState.Outs, &secp256k1fx.TransferOutput{
					Amt: holder.Amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				})
			}
		}
		if len(assetDefinition.InitialState.VariableCap) > 0 {
			for _, owners := range assetDefinition.InitialState.VariableCap {
				out := &secp256k1fx.MintOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
					},
				}
				for _, addrStr := range owners.Minters {
					_, addrBytes, err := address.ParseBech32(addrStr)
					if err != nil {
						return nil, fmt.Errorf("problem parsing minters address: %w", err)
					}
					addr, err := ids.ToShortID(addrBytes)
					if err != nil {
						return nil, fmt.Errorf("problem parsing minters address: %w", err)
					}
					out.Addrs = append(out.Addrs, addr)
				}
				out.Sort()

				initialState.Outs = append(initialState.Outs, out)
			}
		}

		if len(initialState.Outs) > 0 {
			initialState.Sort(genesisCodec)
			asset.States = append(asset.States, initialState)
		}

		utils.Sort(asset.States)
		g.Txs = append(g.Txs, &asset)
	}
	utils.Sort(g.Txs)

	genesisBytes, err := genesisCodec.Marshal(txs.CodecVersion, &g)
	if err != nil {
		return nil, fmt.Errorf("problem marshaling genesis: %w", err)
	}
	return genesisBytes, nil
}

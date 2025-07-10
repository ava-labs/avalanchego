// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"cmp"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
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
	_ utils.Sortable[*GenesisAsset] = (*GenesisAsset)(nil)

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

type Genesis struct {
	Txs []*GenesisAsset `serialize:"true"`
}

type GenesisAsset struct {
	Alias             string `serialize:"true"`
	txs.CreateAssetTx `serialize:"true"`
}

func (g *GenesisAsset) Compare(other *GenesisAsset) int {
	return cmp.Compare(g.Alias, other.Alias)
}

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

// NewGenesis creates a new Genesis from genesis data
func NewGenesis(
	networkID uint32,
	genesisData map[string]AssetDefinition,
) (*Genesis, error) {
	g := &Genesis{}
	for assetAlias, assetDefinition := range genesisData {
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

		if len(initialState.Outs) > 0 {
			codec, err := newGenesisCodec()
			if err != nil {
				return nil, err
			}
			initialState.Sort(codec)
			asset.States = append(asset.States, initialState)
		}

		utils.Sort(asset.States)
		g.Txs = append(g.Txs, &asset)
	}
	utils.Sort(g.Txs)

	return g, nil
}

// Bytes serializes the Genesis to bytes using the AVM genesis codec
func (g *Genesis) Bytes() ([]byte, error) {
	codec, err := newGenesisCodec()
	if err != nil {
		return nil, err
	}
	return codec.Marshal(txs.CodecVersion, g)
}

func newGenesisCodec() (codec.Manager, error) {
	parser, err := txs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
			&nftfx.Fx{},
			&propertyfx.Fx{},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("problem creating parser: %w", err)
	}
	return parser.GenesisCodec(), nil
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

var (
	errUnknownAssetType = errors.New("unknown asset type")

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

// StaticService defines the base service for the asset vm
type StaticService struct{}

func CreateStaticService() *StaticService {
	return &StaticService{}
}

// BuildGenesisArgs are arguments for BuildGenesis
type BuildGenesisArgs struct {
	NetworkID   avajson.Uint32             `json:"networkID"`
	GenesisData map[string]AssetDefinition `json:"genesisData"`
	Encoding    formatting.Encoding        `json:"encoding"`
}

type AssetDefinition struct {
	Name         string                   `json:"name"`
	Symbol       string                   `json:"symbol"`
	Denomination avajson.Uint8            `json:"denomination"`
	InitialState map[string][]interface{} `json:"initialState"`
	Memo         string                   `json:"memo"`
}

// BuildGenesisReply is the reply from BuildGenesis
type BuildGenesisReply struct {
	Bytes    string              `json:"bytes"`
	Encoding formatting.Encoding `json:"encoding"`
}

// BuildGenesis returns the UTXOs such that at least one address in [args.Addresses] is
// referenced in the UTXO.
func (*StaticService) BuildGenesis(_ *http.Request, args *BuildGenesisArgs, reply *BuildGenesisReply) error {
	parser, err := txs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
			&nftfx.Fx{},
			&propertyfx.Fx{},
		},
	)
	if err != nil {
		return err
	}

	g := Genesis{}
	genesisCodec := parser.GenesisCodec()
	for assetAlias, assetDefinition := range args.GenesisData {
		assetMemo, err := formatting.Decode(args.Encoding, assetDefinition.Memo)
		if err != nil {
			return fmt.Errorf("problem formatting asset definition memo due to: %w", err)
		}
		asset := GenesisAsset{
			Alias: assetAlias,
			CreateAssetTx: txs.CreateAssetTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    uint32(args.NetworkID),
					BlockchainID: ids.Empty,
					Memo:         assetMemo,
				}},
				Name:         assetDefinition.Name,
				Symbol:       assetDefinition.Symbol,
				Denomination: byte(assetDefinition.Denomination),
			},
		}
		if len(assetDefinition.InitialState) > 0 {
			initialState := &txs.InitialState{
				FxIndex: 0, // TODO: Should lookup secp256k1fx FxID
			}
			for assetType, initialStates := range assetDefinition.InitialState {
				switch assetType {
				case "fixedCap":
					for _, state := range initialStates {
						b, err := json.Marshal(state)
						if err != nil {
							return fmt.Errorf("problem marshaling state: %w", err)
						}
						holder := Holder{}
						if err := json.Unmarshal(b, &holder); err != nil {
							return fmt.Errorf("problem unmarshaling holder: %w", err)
						}
						_, addrbuff, err := address.ParseBech32(holder.Address)
						if err != nil {
							return fmt.Errorf("problem parsing holder address: %w", err)
						}
						addr, err := ids.ToShortID(addrbuff)
						if err != nil {
							return fmt.Errorf("problem parsing holder address: %w", err)
						}
						initialState.Outs = append(initialState.Outs, &secp256k1fx.TransferOutput{
							Amt: uint64(holder.Amount),
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{addr},
							},
						})
					}
				case "variableCap":
					for _, state := range initialStates {
						b, err := json.Marshal(state)
						if err != nil {
							return fmt.Errorf("problem marshaling state: %w", err)
						}
						owners := Owners{}
						if err := json.Unmarshal(b, &owners); err != nil {
							return fmt.Errorf("problem unmarshaling Owners: %w", err)
						}

						out := &secp256k1fx.MintOutput{
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
							},
						}
						for _, addrStr := range owners.Minters {
							_, addrBytes, err := address.ParseBech32(addrStr)
							if err != nil {
								return fmt.Errorf("problem parsing minters address: %w", err)
							}
							addr, err := ids.ToShortID(addrBytes)
							if err != nil {
								return fmt.Errorf("problem parsing minters address: %w", err)
							}
							out.Addrs = append(out.Addrs, addr)
						}
						out.Sort()

						initialState.Outs = append(initialState.Outs, out)
					}
				default:
					return errUnknownAssetType
				}
			}
			initialState.Sort(genesisCodec)
			asset.States = append(asset.States, initialState)
		}
		utils.Sort(asset.States)
		g.Txs = append(g.Txs, &asset)
	}
	utils.Sort(g.Txs)

	b, err := genesisCodec.Marshal(txs.CodecVersion, &g)
	if err != nil {
		return fmt.Errorf("problem marshaling genesis: %w", err)
	}

	reply.Bytes, err = formatting.Encode(args.Encoding, b)
	if err != nil {
		return fmt.Errorf("couldn't encode genesis as string: %w", err)
	}
	reply.Encoding = args.Encoding
	return nil
}

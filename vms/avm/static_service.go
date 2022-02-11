// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	cjson "github.com/ava-labs/avalanchego/utils/json"
)

var (
	errUnknownAssetType = errors.New("unknown asset type")

	_ avax.TransferableIn  = &secp256k1fx.TransferInput{}
	_ verify.State         = &secp256k1fx.MintOutput{}
	_ avax.TransferableOut = &secp256k1fx.TransferOutput{}
	_ FxOperation          = &secp256k1fx.MintOperation{}
	_ verify.Verifiable    = &secp256k1fx.Credential{}

	_ verify.State      = &nftfx.MintOutput{}
	_ verify.State      = &nftfx.TransferOutput{}
	_ FxOperation       = &nftfx.MintOperation{}
	_ FxOperation       = &nftfx.TransferOperation{}
	_ verify.Verifiable = &nftfx.Credential{}

	_ verify.State      = &propertyfx.MintOutput{}
	_ verify.State      = &propertyfx.OwnedOutput{}
	_ FxOperation       = &propertyfx.MintOperation{}
	_ FxOperation       = &propertyfx.BurnOperation{}
	_ verify.Verifiable = &propertyfx.Credential{}
)

// StaticService defines the base service for the asset vm
type StaticService struct{}

func CreateStaticService() *StaticService {
	return &StaticService{}
}

// BuildGenesisArgs are arguments for BuildGenesis
type BuildGenesisArgs struct {
	NetworkID   cjson.Uint32               `json:"networkID"`
	GenesisData map[string]AssetDefinition `json:"genesisData"`
	Encoding    formatting.Encoding        `json:"encoding"`
}

type AssetDefinition struct {
	Name         string                   `json:"name"`
	Symbol       string                   `json:"symbol"`
	Denomination cjson.Uint8              `json:"denomination"`
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
func (ss *StaticService) BuildGenesis(_ *http.Request, args *BuildGenesisArgs, reply *BuildGenesisReply) error {
	manager, err := staticCodec()
	if err != nil {
		return err
	}

	g := Genesis{}
	for assetAlias, assetDefinition := range args.GenesisData {
		assetMemo, err := formatting.Decode(args.Encoding, assetDefinition.Memo)
		if err != nil {
			return fmt.Errorf("problem formatting asset definition memo due to: %w", err)
		}
		asset := GenesisAsset{
			Alias: assetAlias,
			CreateAssetTx: CreateAssetTx{
				BaseTx: BaseTx{BaseTx: avax.BaseTx{
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
			initialState := &InitialState{
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
						_, addrbuff, err := formatting.ParseBech32(holder.Address)
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
						for _, address := range owners.Minters {
							_, addrbuff, err := formatting.ParseBech32(address)
							if err != nil {
								return fmt.Errorf("problem parsing minters address: %w", err)
							}
							addr, err := ids.ToShortID(addrbuff)
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
			initialState.Sort(manager)
			asset.States = append(asset.States, initialState)
		}
		asset.Sort()
		g.Txs = append(g.Txs, &asset)
	}
	g.Sort()

	b, err := manager.Marshal(codecVersion, &g)
	if err != nil {
		return fmt.Errorf("problem marshaling genesis: %w", err)
	}

	reply.Bytes, err = formatting.EncodeWithChecksum(args.Encoding, b)
	if err != nil {
		return fmt.Errorf("couldn't encode genesis as string: %w", err)
	}
	reply.Encoding = args.Encoding
	return nil
}

func staticCodec() (codec.Manager, error) {
	c := linearcodec.New(reflectcodec.DefaultTagName, 1<<20)
	manager := codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&BaseTx{}),
		c.RegisterType(&CreateAssetTx{}),
		c.RegisterType(&OperationTx{}),
		c.RegisterType(&ImportTx{}),
		c.RegisterType(&ExportTx{}),
		c.RegisterType(&secp256k1fx.TransferInput{}),
		c.RegisterType(&secp256k1fx.MintOutput{}),
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		c.RegisterType(&secp256k1fx.MintOperation{}),
		c.RegisterType(&secp256k1fx.Credential{}),
		c.RegisterType(&nftfx.MintOutput{}),
		c.RegisterType(&nftfx.TransferOutput{}),
		c.RegisterType(&nftfx.MintOperation{}),
		c.RegisterType(&nftfx.TransferOperation{}),
		c.RegisterType(&nftfx.Credential{}),
		c.RegisterType(&propertyfx.MintOutput{}),
		c.RegisterType(&propertyfx.OwnedOutput{}),
		c.RegisterType(&propertyfx.MintOperation{}),
		c.RegisterType(&propertyfx.BurnOperation{}),
		c.RegisterType(&propertyfx.Credential{}),
		manager.RegisterCodec(codecVersion, c),
	)
	return manager, errs.Err
}

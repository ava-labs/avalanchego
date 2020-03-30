// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/wrappers"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	cjson "github.com/ava-labs/gecko/utils/json"
)

var (
	errUnknownAssetType = errors.New("unknown asset type")
)

// StaticService defines the base service for the asset vm
type StaticService struct{}

// BuildGenesisArgs are arguments for BuildGenesis
type BuildGenesisArgs struct {
	GenesisData map[string]AssetDefinition `json:"genesisData"`
}

// AssetDefinition ...
type AssetDefinition struct {
	Name         string                   `json:"name"`
	Symbol       string                   `json:"symbol"`
	Denomination cjson.Uint8              `json:"denomination"`
	InitialState map[string][]interface{} `json:"initialState"`
}

// BuildGenesisReply is the reply from BuildGenesis
type BuildGenesisReply struct {
	Bytes formatting.CB58 `json:"bytes"`
}

// BuildGenesis returns the UTXOs such that at least one address in [args.Addresses] is
// referenced in the UTXO.
func (*StaticService) BuildGenesis(_ *http.Request, args *BuildGenesisArgs, reply *BuildGenesisReply) error {
	errs := wrappers.Errs{}

	c := codec.NewDefault()
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
	)
	if errs.Errored() {
		return errs.Err
	}

	g := Genesis{}
	for assetAlias, assetDefinition := range args.GenesisData {
		asset := GenesisAsset{
			Alias: assetAlias,
			CreateAssetTx: CreateAssetTx{
				BaseTx: BaseTx{
					BCID: ids.Empty,
				},
				Name:         assetDefinition.Name,
				Symbol:       assetDefinition.Symbol,
				Denomination: byte(assetDefinition.Denomination),
			},
		}
		for assetType, initialStates := range assetDefinition.InitialState {
			switch assetType {
			case "fixedCap":
				initialState := &InitialState{
					FxID: 0, // TODO: Should lookup secp256k1fx FxID
				}
				for _, state := range initialStates {
					b, err := json.Marshal(state)
					if err != nil {
						return err
					}
					holder := Holder{}
					if err := json.Unmarshal(b, &holder); err != nil {
						return err
					}
					cb58 := formatting.CB58{}
					if err := cb58.FromString(holder.Address); err != nil {
						return err
					}
					addr, err := ids.ToShortID(cb58.Bytes)
					if err != nil {
						return err
					}
					initialState.Outs = append(initialState.Outs, &secp256k1fx.TransferOutput{
						Amt: uint64(holder.Amount),
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addr},
						},
					})
				}
				initialState.Sort(c)
				asset.States = append(asset.States, initialState)
			case "variableCap":
				initialState := &InitialState{
					FxID: 0, // TODO: Should lookup secp256k1fx FxID
				}
				for _, state := range initialStates {
					b, err := json.Marshal(state)
					if err != nil {
						return err
					}
					owners := Owners{}
					if err := json.Unmarshal(b, &owners); err != nil {
						return err
					}

					out := &secp256k1fx.MintOutput{
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
						},
					}
					for _, address := range owners.Minters {
						cb58 := formatting.CB58{}
						if err := cb58.FromString(address); err != nil {
							return err
						}
						addr, err := ids.ToShortID(cb58.Bytes)
						if err != nil {
							return err
						}
						out.Addrs = append(out.Addrs, addr)
					}
					out.Sort()

					initialState.Outs = append(initialState.Outs, out)
				}
				initialState.Sort(c)
				asset.States = append(asset.States, initialState)
			default:
				return errUnknownAssetType
			}
		}
		asset.Sort()
		g.Txs = append(g.Txs, &asset)
	}
	g.Sort()

	b, err := c.Marshal(&g)
	if err != nil {
		return err
	}

	reply.Bytes.Bytes = b
	return nil
}

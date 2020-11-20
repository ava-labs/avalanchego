package static

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/codec"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/avm/internalvm"
	"github.com/ava-labs/avalanchego/vms/avm/vmargs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errUnknownAssetType = errors.New("unknown asset type")
)

// BuildGenesis returns the UTXOs such that at least one address in [args.Addresses] is
// referenced in the UTXO.
func BuildGenesis(args *vmargs.BuildGenesisArgs, reply *vmargs.BuildGenesisReply) error {
	errs := wrappers.Errs{}

	c := codec.New(math.MaxUint32, 1<<20)
	errs.Add(
		c.RegisterType(&internalvm.BaseTx{}),
		c.RegisterType(&internalvm.CreateAssetTx{}),
		c.RegisterType(&internalvm.OperationTx{}),
		c.RegisterType(&internalvm.ImportTx{}),
		c.RegisterType(&internalvm.ExportTx{}),
		c.RegisterType(&secp256k1fx.TransferInput{}),
		c.RegisterType(&secp256k1fx.MintOutput{}),
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		c.RegisterType(&secp256k1fx.MintOperation{}),
		c.RegisterType(&secp256k1fx.Credential{}),
	)
	if errs.Errored() {
		return errs.Err
	}

	encodingManager, err := formatting.NewEncodingManager(args.Encoding)
	if err != nil {
		fmt.Errorf("problem creating avm Static Service: %w", err)
	}

	encoding, err := encodingManager.GetEncoding(args.Encoding)
	if err != nil {
		return fmt.Errorf("problem getting encoding formatter for '%s': %w", args.Encoding, err)
	}

	g := internalvm.Genesis{}
	for assetAlias, assetDefinition := range args.GenesisData {
		assetMemo, err := encoding.ConvertString(assetDefinition.Memo)
		if err != nil {
			return fmt.Errorf("problem formatting asset definition memo due to: %w", err)
		}
		asset := internalvm.GenesisAsset{
			Alias: assetAlias,
			CreateAssetTx: internalvm.CreateAssetTx{
				BaseTx: internalvm.BaseTx{BaseTx: avax.BaseTx{
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
			initialState := &internalvm.InitialState{
				FxID: 0, // TODO: Should lookup secp256k1fx FxID
			}
			for assetType, initialStates := range assetDefinition.InitialState {
				switch assetType {
				case "fixedCap":
					for _, state := range initialStates {
						b, err := json.Marshal(state)
						if err != nil {
							return fmt.Errorf("problem marshaling state: %w", err)
						}
						holder := internalvm.Holder{}
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
						owners := internalvm.Owners{}
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
			initialState.Sort(c)
			asset.States = append(asset.States, initialState)
		}
		asset.Sort()
		g.Txs = append(g.Txs, &asset)
	}
	g.Sort()

	b, err := c.Marshal(&g)
	if err != nil {
		return fmt.Errorf("problem marshaling genesis: %w", err)
	}

	reply.Bytes = encoding.ConvertBytes(b)
	reply.Encoding = encoding.Encoding()
	return nil
}

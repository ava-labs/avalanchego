// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fees"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	testUnitFees = fees.Dimensions{
		1 * units.MicroAvax,
		2 * units.MicroAvax,
		3 * units.MicroAvax,
		4 * units.MicroAvax,
	}
	testBlockMaxConsumedUnits = fees.Dimensions{
		3000,
		3500,
		1000,
		2000,
	}

	feeTestsDefaultCfg = config.Config{
		TxFee:            1 * units.Avax,
		CreateAssetTxFee: 2 * units.Avax,
	}

	feeTestKeys    = secp256k1.TestKeys()
	feeTestSigners = [][]*secp256k1.PrivateKey{}
)

type feeTests struct {
	description       string
	cfgAndChainTimeF  func() (*config.Config, time.Time)
	consumedUnitCapsF func() fees.Dimensions
	expectedError     error
	checksF           func(*testing.T, *Calculator)
}

func TestBaseTxFees(t *testing.T) {
	r := require.New(t)

	defaultCtx := snowtest.Context(t, snowtest.PChainID)
	codec, err := createTestFeesCodec(defaultCtx)
	r.NoError(err)

	baseTx, _ := txsCreationHelpers(defaultCtx)
	uTx := &baseTx
	sTx := &txs.Tx{
		Unsigned: uTx,
	}
	r.NoError(sTx.SignSECP256K1Fx(codec, feeTestSigners))

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 4920*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						224,
						90,
						172,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime
				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			expectedError: errFailedConsumedUnitsCumulation,
			checksF:       func(*testing.T, *Calculator) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			cfg, chainTime := tt.cfgAndChainTimeF()

			consumedUnitCaps := testBlockMaxConsumedUnits
			if tt.consumedUnitCapsF != nil {
				consumedUnitCaps = tt.consumedUnitCapsF()
			}

			fc := &Calculator{
				IsEUpgradeActive: cfg.IsEActivated(chainTime),
				Config:           cfg,
				Codec:            codec,
				FeeManager:       fees.NewManager(testUnitFees),
				ConsumedUnitsCap: consumedUnitCaps,
				Credentials:      sTx.Creds,
			}
			err := uTx.Visit(fc)
			r.ErrorIs(err, tt.expectedError)
			tt.checksF(t, fc)
		})
	}
}

func TestCreateAssetTxFees(t *testing.T) {
	r := require.New(t)

	defaultCtx := snowtest.Context(t, snowtest.PChainID)
	codec, err := createTestFeesCodec(defaultCtx)
	r.NoError(err)

	baseTx, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.CreateAssetTx{
		BaseTx:       baseTx,
		Name:         "name",
		Symbol:       "symb",
		Denomination: 0,
		States: []*txs.InitialState{
			{
				FxIndex: 0,
				Outs: []verify.State{
					&secp256k1fx.MintOutput{
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{feeTestKeys[0].PublicKey().Address()},
						},
					},
				},
			},
		},
	}
	sTx := &txs.Tx{
		Unsigned: uTx,
	}
	r.NoError(sTx.SignSECP256K1Fx(codec, feeTestSigners))

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.CreateAssetTxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 4985*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						289,
						90,
						172,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "post E fork, bandwidth cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime
				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.Bandwidth] = 289 - 1
				return caps
			},
			expectedError: errFailedConsumedUnitsCumulation,
			checksF:       func(*testing.T, *Calculator) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			cfg, chainTime := tt.cfgAndChainTimeF()

			consumedUnitCaps := testBlockMaxConsumedUnits
			if tt.consumedUnitCapsF != nil {
				consumedUnitCaps = tt.consumedUnitCapsF()
			}

			fc := &Calculator{
				IsEUpgradeActive: cfg.IsEActivated(chainTime),
				Config:           cfg,
				Codec:            codec,
				FeeManager:       fees.NewManager(testUnitFees),
				ConsumedUnitsCap: consumedUnitCaps,
				Credentials:      sTx.Creds,
			}
			err := uTx.Visit(fc)
			r.ErrorIs(err, tt.expectedError)
			tt.checksF(t, fc)
		})
	}
}

func TestOperationTxFees(t *testing.T) {
	r := require.New(t)

	defaultCtx := snowtest.Context(t, snowtest.PChainID)
	codec, err := createTestFeesCodec(defaultCtx)
	r.NoError(err)

	baseTx, _ := txsCreationHelpers(defaultCtx)
	assetID := ids.GenerateTestID()
	uTx := &txs.OperationTx{
		BaseTx: baseTx,
		Ops: []*txs.Operation{{
			Asset: avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{{
				TxID:        assetID,
				OutputIndex: 1,
			}},
			Op: &nftfx.MintOperation{
				MintInput: secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
				GroupID: 1,
				Payload: []byte{'h', 'e', 'l', 'l', 'o'},
				Outputs: []*secp256k1fx.OutputOwners{{}},
			},
		}},
	}
	sTx := &txs.Tx{
		Unsigned: uTx,
	}
	r.NoError(sTx.SignSECP256K1Fx(codec, feeTestSigners))

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5041*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						345,
						90,
						172,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime
				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			expectedError: errFailedConsumedUnitsCumulation,
			checksF:       func(*testing.T, *Calculator) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			cfg, chainTime := tt.cfgAndChainTimeF()

			consumedUnitCaps := testBlockMaxConsumedUnits
			if tt.consumedUnitCapsF != nil {
				consumedUnitCaps = tt.consumedUnitCapsF()
			}

			fc := &Calculator{
				IsEUpgradeActive: cfg.IsEActivated(chainTime),
				Config:           cfg,
				Codec:            codec,
				FeeManager:       fees.NewManager(testUnitFees),
				ConsumedUnitsCap: consumedUnitCaps,
				Credentials:      sTx.Creds,
			}
			err := uTx.Visit(fc)
			r.ErrorIs(err, tt.expectedError)
			tt.checksF(t, fc)
		})
	}
}

func TestImportTxFees(t *testing.T) {
	r := require.New(t)

	defaultCtx := snowtest.Context(t, snowtest.PChainID)
	codec, err := createTestFeesCodec(defaultCtx)
	r.NoError(err)

	baseTx, _ := txsCreationHelpers(defaultCtx)
	uTx := &txs.ImportTx{
		BaseTx:      baseTx,
		SourceChain: ids.GenerateTestID(),
		ImportedIns: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
			In: &secp256k1fx.TransferInput{
				Amt:   50000,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
	}
	sTx := &txs.Tx{
		Unsigned: uTx,
	}
	r.NoError(sTx.SignSECP256K1Fx(codec, feeTestSigners))

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 9494*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						348,
						180,
						262,
						2000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime
				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 180 - 1
				return caps
			},
			expectedError: errFailedConsumedUnitsCumulation,
			checksF:       func(*testing.T, *Calculator) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			cfg, chainTime := tt.cfgAndChainTimeF()

			consumedUnitCaps := testBlockMaxConsumedUnits
			if tt.consumedUnitCapsF != nil {
				consumedUnitCaps = tt.consumedUnitCapsF()
			}

			fc := &Calculator{
				IsEUpgradeActive: cfg.IsEActivated(chainTime),
				Config:           cfg,
				Codec:            codec,
				FeeManager:       fees.NewManager(testUnitFees),
				ConsumedUnitsCap: consumedUnitCaps,
				Credentials:      sTx.Creds,
			}
			err := uTx.Visit(fc)
			r.ErrorIs(err, tt.expectedError)
			tt.checksF(t, fc)
		})
	}
}

func TestExportTxFees(t *testing.T) {
	r := require.New(t)

	defaultCtx := snowtest.Context(t, snowtest.PChainID)
	codec, err := createTestFeesCodec(defaultCtx)
	r.NoError(err)

	baseTx, outputs := txsCreationHelpers(defaultCtx)
	uTx := &txs.ExportTx{
		BaseTx:           baseTx,
		DestinationChain: ids.GenerateTestID(),
		ExportedOuts:     outputs,
	}
	sTx := &txs.Tx{
		Unsigned: uTx,
	}
	r.NoError(sTx.SignSECP256K1Fx(codec, feeTestSigners))

	tests := []feeTests{
		{
			description: "pre E fork",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(-1 * time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, fc.Config.TxFee, fc.Fee)
			},
		},
		{
			description: "post E fork, success",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime

				return &cfg, chainTime
			},
			expectedError: nil,
			checksF: func(t *testing.T, fc *Calculator) {
				require.Equal(t, 5282*units.MicroAvax, fc.Fee)
				require.Equal(t,
					fees.Dimensions{
						340,
						90,
						254,
						1000,
					},
					fc.FeeManager.GetCumulatedComplexity(),
				)
			},
		},
		{
			description: "post E fork, utxos read cap breached",
			cfgAndChainTimeF: func() (*config.Config, time.Time) {
				eForkTime := time.Now().Truncate(time.Second)
				chainTime := eForkTime.Add(time.Second)

				cfg := feeTestsDefaultCfg
				cfg.EUpgradeTime = eForkTime
				return &cfg, chainTime
			},
			consumedUnitCapsF: func() fees.Dimensions {
				caps := testBlockMaxConsumedUnits
				caps[fees.UTXORead] = 90 - 1
				return caps
			},
			expectedError: errFailedConsumedUnitsCumulation,
			checksF:       func(*testing.T, *Calculator) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			cfg, chainTime := tt.cfgAndChainTimeF()

			consumedUnitCaps := testBlockMaxConsumedUnits
			if tt.consumedUnitCapsF != nil {
				consumedUnitCaps = tt.consumedUnitCapsF()
			}

			fc := &Calculator{
				IsEUpgradeActive: cfg.IsEActivated(chainTime),
				Config:           cfg,
				Codec:            codec,
				FeeManager:       fees.NewManager(testUnitFees),
				ConsumedUnitsCap: consumedUnitCaps,
				Credentials:      sTx.Creds,
			}
			err := uTx.Visit(fc)
			r.ErrorIs(err, tt.expectedError)
			tt.checksF(t, fc)
		})
	}
}

func createTestFeesCodec(defaultCtx *snow.Context) (codec.Manager, error) {
	fxs := []fxs.Fx{
		&secp256k1fx.Fx{},
		&nftfx.Fx{},
	}

	parser, err := block.NewCustomParser(
		map[reflect.Type]int{},
		&mockable.Clock{},
		defaultCtx.Log,
		fxs,
	)
	if err != nil {
		return nil, err
	}

	return parser.Codec(), nil
}

func txsCreationHelpers(defaultCtx *snow.Context) (
	baseTx txs.BaseTx,
	otherOutputs []*avax.TransferableOutput,
) {
	inputs := []*avax.TransferableInput{{
		UTXOID: avax.UTXOID{
			TxID:        ids.ID{'t', 'x', 'I', 'D'},
			OutputIndex: 2,
		},
		Asset: avax.Asset{ID: defaultCtx.AVAXAssetID},
		In: &secp256k1fx.TransferInput{
			Amt:   uint64(5678),
			Input: secp256k1fx.Input{SigIndices: []uint32{0}},
		},
	}}
	outputs := []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: defaultCtx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(1234),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{feeTestKeys[0].PublicKey().Address()},
			},
		},
	}}
	otherOutputs = []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: defaultCtx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(4567),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{feeTestKeys[1].PublicKey().Address()},
			},
		},
	}}
	baseTx = txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    defaultCtx.NetworkID,
			BlockchainID: defaultCtx.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		},
	}

	return baseTx, otherOutputs
}

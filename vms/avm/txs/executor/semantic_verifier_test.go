// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/state"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
	commonfee "github.com/ava-labs/avalanchego/vms/components/fee"
)

var feeConfig = config.Config{
	StaticConfig: fee.StaticConfig{
		TxFee:            2 * units.MilliAvax,
		CreateAssetTxFee: 3 * units.MilliAvax,
	},
	EUpgradeTime: time.Time{},
}

func TestSemanticVerifierBaseTx(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.XChainID)

	// UTXO to be spent
	inputTxID := ids.GenerateTestID()
	utxoID := avax.UTXOID{
		TxID:        inputTxID,
		OutputIndex: 0,
	}

	feeAssetID := ids.GenerateTestID()
	asset := avax.Asset{
		ID: feeAssetID,
	}
	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
	}
	utxoAmount := units.Avax
	utxoOut := secp256k1fx.TransferOutput{
		Amt:          utxoAmount,
		OutputOwners: outputOwners,
	}
	utxo := avax.UTXO{
		UTXOID: utxoID,
		Asset:  asset,
		Out:    &utxoOut,
	}

	// Input spending the UTXO
	inputSigners := secp256k1fx.Input{
		SigIndices: []uint32{0},
	}
	fxInput := secp256k1fx.TransferInput{
		Amt:   utxoAmount,
		Input: inputSigners,
	}
	input := avax.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In:     &fxInput,
	}

	// Output produced by BaseTx
	fxOutput := secp256k1fx.TransferOutput{
		Amt:          100,
		OutputOwners: outputOwners,
	}
	output := avax.TransferableOutput{
		Asset: asset,
		Out:   &fxOutput,
	}

	// BaseTx
	baseTx := avax.BaseTx{
		Outs: []*avax.TransferableOutput{
			&output,
		},
		Ins: []*avax.TransferableInput{
			&input,
		},
	}

	// Backend
	typeToFxIndex := make(map[reflect.Type]int)
	secpFx := &secp256k1fx.Fx{}
	parser, err := txs.NewCustomParser(
		typeToFxIndex,
		new(mockable.Clock),
		logging.NoWarn{},
		[]fxs.Fx{
			secpFx,
		},
	)
	require.NoError(t, err)
	codec := parser.Codec()
	backend := &Backend{
		Ctx:    ctx,
		Config: &feeConfig,
		Fxs: []*fxs.ParsedFx{
			{
				ID: secp256k1fx.ID,
				Fx: secpFx,
			},
		},
		TypeToFxIndex: typeToFxIndex,
		Codec:         codec,
		FeeAssetID:    feeAssetID,
		Bootstrapped:  true,
	}
	require.NoError(t, secpFx.Bootstrapped())

	unsignedCreateAssetTx := txs.CreateAssetTx{
		States: []*txs.InitialState{{
			FxIndex: 0,
		}},
	}
	createAssetTx := txs.Tx{
		Unsigned: &unsignedCreateAssetTx,
	}

	tests := []struct {
		name      string
		stateFunc func(*gomock.Controller) state.Chain
		txFunc    func(*require.Assertions) *txs.Tx
		err       error
	}{
		{
			name: "valid",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).Times(2)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: baseTx,
					},
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: nil,
		},
		{
			name: "invalid output",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				output := output
				output.Out = &secp256k1fx.TransferOutput{
					Amt:          0,
					OutputOwners: outputOwners,
				}

				baseTx := baseTx
				baseTx.Outs = []*avax.TransferableOutput{
					&output,
				}

				tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: baseTx}}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrNoValueOutput,
		},
		{
			name: "unsorted outputs",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				output0 := output
				output0.Out = &secp256k1fx.TransferOutput{
					Amt:          1,
					OutputOwners: outputOwners,
				}

				output1 := output
				output1.Out = &secp256k1fx.TransferOutput{
					Amt:          2,
					OutputOwners: outputOwners,
				}

				outputs := []*avax.TransferableOutput{
					&output0,
					&output1,
				}
				avax.SortTransferableOutputs(outputs, codec)
				outputs[0], outputs[1] = outputs[1], outputs[0]

				baseTx := baseTx
				baseTx.Outs = outputs

				tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: baseTx}}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: avax.ErrOutputsNotSorted,
		},
		{
			name: "invalid input",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input := input
				input.In = &secp256k1fx.TransferInput{
					Amt:   0,
					Input: inputSigners,
				}

				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input,
				}

				tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: baseTx}}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrNoValueInput,
		},
		{
			name: "duplicate inputs",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input,
					&input,
				}

				tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: baseTx}}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
						{keys[0]},
					},
				))
				return tx
			},
			err: avax.ErrInputsNotSortedUnique,
		},
		{
			name: "input overflow",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input0 := input
				input0.In = &secp256k1fx.TransferInput{
					Amt:   1,
					Input: inputSigners,
				}

				input1 := input
				input1.UTXOID.OutputIndex++
				input1.In = &secp256k1fx.TransferInput{
					Amt:   math.MaxUint64,
					Input: inputSigners,
				}

				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input0,
					&input1,
				}
				avax.SortTransferableInputsWithSigners(baseTx.Ins, make([][]*secp256k1.PrivateKey, 2))

				tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: baseTx}}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: safemath.ErrOverflow,
		},
		{
			name: "output overflow",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				output0 := output
				output0.Out = &secp256k1fx.TransferOutput{
					Amt:          1,
					OutputOwners: outputOwners,
				}

				output1 := output
				output1.Out = &secp256k1fx.TransferOutput{
					Amt:          math.MaxUint64,
					OutputOwners: outputOwners,
				}

				outputs := []*avax.TransferableOutput{
					&output0,
					&output1,
				}
				avax.SortTransferableOutputs(outputs, codec)

				baseTx := baseTx
				baseTx.Outs = outputs

				tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: baseTx}}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: safemath.ErrOverflow,
		},
		{
			name: "barely sufficient funds",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				utxoAmount := 1650 * units.NanoAvax
				utxoOut := secp256k1fx.TransferOutput{
					Amt:          utxoAmount,
					OutputOwners: outputOwners,
				}
				utxo := avax.UTXO{
					UTXOID: utxoID,
					Asset:  asset,
					Out:    &utxoOut,
				}

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).Times(2)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input := input
				input.In = &secp256k1fx.TransferInput{
					Amt:   1650,
					Input: inputSigners,
				}

				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input,
				}

				tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: baseTx}}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: nil,
		},
		{
			name: "insufficient funds",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input := input
				input.In = &secp256k1fx.TransferInput{
					Amt:   1,
					Input: inputSigners,
				}

				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input,
				}

				tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: baseTx}}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: avax.ErrInsufficientFunds,
		},
		{
			name: "barely insufficient funds",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input := input
				input.In = &secp256k1fx.TransferInput{
					Amt:   1649 * units.NanoAvax,
					Input: inputSigners,
				}

				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input,
				}

				tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: baseTx}}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: avax.ErrInsufficientFunds,
		},
		{
			name: "assetID mismatch",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				utxo := utxo
				utxo.Asset.ID = ids.GenerateTestID()

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: baseTx,
					},
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errAssetIDMismatch,
		},
		{
			name: "not allowed input feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: baseTx,
					},
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "invalid signature",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: baseTx,
					},
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[1]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrWrongSig,
		},
		{
			name: "missing UTXO",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: baseTx,
					},
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "invalid UTXO amount",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				utxoOut := utxoOut
				utxoOut.Amt--

				utxo := utxo
				utxo.Out = &utxoOut

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: baseTx,
					},
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrMismatchedAmounts,
		},
		{
			name: "not allowed output feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: baseTx,
					},
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "unknown asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: baseTx,
					},
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "not an asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				tx := txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: baseTx,
					},
				}

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&tx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: baseTx,
					},
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errNotAnAsset,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			chain := test.stateFunc(ctrl)
			tx := test.txFunc(require)

			feeCalc, err := state.PickFeeCalculator(&feeConfig, parser.Codec(), chain, chain.GetTimestamp())
			require.NoError(err)

			err = tx.Unsigned.Visit(&SemanticVerifier{
				Backend:       backend,
				FeeCalculator: feeCalc,
				State:         chain,
				Tx:            tx,
			})
			require.ErrorIs(err, test.err)
		})
	}
}

func TestSemanticVerifierExportTx(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.XChainID)

	// UTXO to be spent
	inputTxID := ids.GenerateTestID()
	utxoID := avax.UTXOID{
		TxID:        inputTxID,
		OutputIndex: 0,
	}

	feeAssetID := ids.GenerateTestID()
	asset := avax.Asset{
		ID: feeAssetID,
	}
	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
	}
	utxoAmount := units.Avax
	utxoOut := secp256k1fx.TransferOutput{
		Amt:          utxoAmount,
		OutputOwners: outputOwners,
	}
	utxo := avax.UTXO{
		UTXOID: utxoID,
		Asset:  asset,
		Out:    &utxoOut,
	}

	// Input spending the UTXO
	inputSigners := secp256k1fx.Input{
		SigIndices: []uint32{0},
	}
	fxInput := secp256k1fx.TransferInput{
		Amt:   utxoAmount,
		Input: inputSigners,
	}
	input := avax.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In:     &fxInput,
	}

	// Output produced by BaseTx
	fxOutput := secp256k1fx.TransferOutput{
		Amt:          100,
		OutputOwners: outputOwners,
	}
	output := avax.TransferableOutput{
		Asset: asset,
		Out:   &fxOutput,
	}

	baseTx := avax.BaseTx{
		Outs: []*avax.TransferableOutput{
			&output,
		},
		Ins: []*avax.TransferableInput{
			&input,
		},
	}

	exportTx := txs.ExportTx{
		BaseTx: txs.BaseTx{
			BaseTx: baseTx,
		},
		DestinationChain: ctx.CChainID,
	}

	typeToFxIndex := make(map[reflect.Type]int)
	secpFx := &secp256k1fx.Fx{}
	parser, err := txs.NewCustomParser(
		typeToFxIndex,
		new(mockable.Clock),
		logging.NoWarn{},
		[]fxs.Fx{
			secpFx,
		},
	)
	require.NoError(t, err)
	codec := parser.Codec()
	backend := &Backend{
		Ctx:    ctx,
		Config: &feeConfig,
		Fxs: []*fxs.ParsedFx{
			{
				ID: secp256k1fx.ID,
				Fx: secpFx,
			},
		},
		TypeToFxIndex: typeToFxIndex,
		Codec:         codec,
		FeeAssetID:    feeAssetID,
		Bootstrapped:  true,
	}
	require.NoError(t, secpFx.Bootstrapped())

	unsignedCreateAssetTx := txs.CreateAssetTx{
		States: []*txs.InitialState{{
			FxIndex: 0,
		}},
	}
	createAssetTx := txs.Tx{
		Unsigned: &unsignedCreateAssetTx,
	}

	tests := []struct {
		name      string
		stateFunc func(*gomock.Controller) state.Chain
		txFunc    func(*require.Assertions) *txs.Tx
		err       error
	}{
		{
			name: "valid",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).Times(2)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: nil,
		},
		{
			name: "invalid output",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				output := output
				output.Out = &secp256k1fx.TransferOutput{
					Amt:          0,
					OutputOwners: outputOwners,
				}

				baseTx := baseTx
				baseTx.Outs = []*avax.TransferableOutput{
					&output,
				}

				eTx := exportTx
				eTx.BaseTx.BaseTx = baseTx
				tx := &txs.Tx{Unsigned: &eTx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrNoValueOutput,
		},
		{
			name: "unsorted outputs",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				output0 := output
				output0.Out = &secp256k1fx.TransferOutput{
					Amt:          1,
					OutputOwners: outputOwners,
				}

				output1 := output
				output1.Out = &secp256k1fx.TransferOutput{
					Amt:          2,
					OutputOwners: outputOwners,
				}

				outputs := []*avax.TransferableOutput{
					&output0,
					&output1,
				}
				avax.SortTransferableOutputs(outputs, codec)
				outputs[0], outputs[1] = outputs[1], outputs[0]

				baseTx := baseTx
				baseTx.Outs = outputs

				eTx := exportTx
				eTx.BaseTx.BaseTx = baseTx
				tx := &txs.Tx{Unsigned: &eTx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: avax.ErrOutputsNotSorted,
		},
		{
			name: "unsorted exported outputs",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				output0 := output
				output0.Out = &secp256k1fx.TransferOutput{
					Amt:          1,
					OutputOwners: outputOwners,
				}

				output1 := output
				output1.Out = &secp256k1fx.TransferOutput{
					Amt:          2,
					OutputOwners: outputOwners,
				}

				outputs := []*avax.TransferableOutput{
					&output0,
					&output1,
				}
				avax.SortTransferableOutputs(outputs, codec)
				outputs[0], outputs[1] = outputs[1], outputs[0]

				utx := exportTx
				utx.ExportedOuts = outputs
				tx := &txs.Tx{Unsigned: &utx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: avax.ErrOutputsNotSorted,
		},
		{
			name: "invalid input",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input := input
				input.In = &secp256k1fx.TransferInput{
					Amt:   0,
					Input: inputSigners,
				}

				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input,
				}

				eTx := exportTx
				eTx.BaseTx.BaseTx = baseTx
				tx := &txs.Tx{Unsigned: &eTx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrNoValueInput,
		},
		{
			name: "duplicate inputs",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input,
					&input,
				}

				eTx := exportTx
				eTx.BaseTx.BaseTx = baseTx
				tx := &txs.Tx{Unsigned: &eTx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
						{keys[0]},
					},
				))
				return tx
			},
			err: avax.ErrInputsNotSortedUnique,
		},
		{
			name: "input overflow",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input0 := input
				input0.In = &secp256k1fx.TransferInput{
					Amt:   1,
					Input: inputSigners,
				}

				input1 := input
				input1.UTXOID.OutputIndex++
				input1.In = &secp256k1fx.TransferInput{
					Amt:   math.MaxUint64,
					Input: inputSigners,
				}

				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input0,
					&input1,
				}
				avax.SortTransferableInputsWithSigners(baseTx.Ins, make([][]*secp256k1.PrivateKey, 2))

				eTx := exportTx
				eTx.BaseTx.BaseTx = baseTx
				tx := &txs.Tx{Unsigned: &eTx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: safemath.ErrOverflow,
		},
		{
			name: "output overflow",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				output0 := output
				output0.Out = &secp256k1fx.TransferOutput{
					Amt:          1,
					OutputOwners: outputOwners,
				}

				output1 := output
				output1.Out = &secp256k1fx.TransferOutput{
					Amt:          math.MaxUint64,
					OutputOwners: outputOwners,
				}

				outputs := []*avax.TransferableOutput{
					&output0,
					&output1,
				}
				avax.SortTransferableOutputs(outputs, codec)

				baseTx := baseTx
				baseTx.Outs = outputs

				eTx := exportTx
				eTx.BaseTx.BaseTx = baseTx
				tx := &txs.Tx{Unsigned: &eTx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: safemath.ErrOverflow,
		},
		{
			name: "barely sufficient funds",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				utxoAmount := 1690 * units.NanoAvax
				utxoOut := secp256k1fx.TransferOutput{
					Amt:          utxoAmount,
					OutputOwners: outputOwners,
				}
				utxo := avax.UTXO{
					UTXOID: utxoID,
					Asset:  asset,
					Out:    &utxoOut,
				}

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).Times(2)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input := input
				input.In = &secp256k1fx.TransferInput{
					Amt:   1690,
					Input: inputSigners,
				}

				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input,
				}

				eTx := exportTx
				eTx.BaseTx.BaseTx = baseTx
				tx := &txs.Tx{Unsigned: &eTx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: nil,
		},
		{
			name: "insufficient funds",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input := input
				input.In = &secp256k1fx.TransferInput{
					Amt:   1,
					Input: inputSigners,
				}

				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input,
				}

				eTx := exportTx
				eTx.BaseTx.BaseTx = baseTx
				tx := &txs.Tx{Unsigned: &eTx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: avax.ErrInsufficientFunds,
		},
		{
			name: "barely insufficient funds",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input := input
				input.In = &secp256k1fx.TransferInput{
					Amt:   1689 * units.NanoAvax,
					Input: inputSigners,
				}

				baseTx := baseTx
				baseTx.Ins = []*avax.TransferableInput{
					&input,
				}

				eTx := exportTx
				eTx.BaseTx.BaseTx = baseTx
				tx := &txs.Tx{Unsigned: &eTx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: avax.ErrInsufficientFunds,
		},
		{
			name: "assetID mismatch",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				utxo := utxo
				utxo.Asset.ID = ids.GenerateTestID()

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errAssetIDMismatch,
		},
		{
			name: "not allowed input feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "invalid signature",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[1]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrWrongSig,
		},
		{
			name: "missing UTXO",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "invalid UTXO amount",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				output := utxoOut
				output.Amt--

				utxo := utxo
				utxo.Out = &output

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrMismatchedAmounts,
		},
		{
			name: "not allowed output feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "unknown asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "not an asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				tx := txs.Tx{
					Unsigned: &exportTx,
				}

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&tx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errNotAnAsset,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			chain := test.stateFunc(ctrl)
			tx := test.txFunc(require)

			feeCalc, err := state.PickFeeCalculator(&feeConfig, parser.Codec(), chain, chain.GetTimestamp())
			require.NoError(err)

			err = tx.Unsigned.Visit(&SemanticVerifier{
				Backend:       backend,
				FeeCalculator: feeCalc,
				State:         chain,
				Tx:            tx,
			})
			require.ErrorIs(err, test.err)
		})
	}
}

func TestSemanticVerifierExportTxDifferentSubnet(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	ctx := snowtest.Context(t, snowtest.XChainID)

	validatorState := validators.NewMockState(ctrl)
	validatorState.EXPECT().GetSubnetID(gomock.Any(), ctx.CChainID).AnyTimes().Return(ids.GenerateTestID(), nil)
	ctx.ValidatorState = validatorState

	// UTXO to be spent
	inputTxID := ids.GenerateTestID()
	utxoID := avax.UTXOID{
		TxID:        inputTxID,
		OutputIndex: 0,
	}

	feeAssetID := ids.GenerateTestID()
	asset := avax.Asset{
		ID: feeAssetID,
	}
	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
	}
	utxoAmount := 100 + feeConfig.TxFee
	utxoOut := secp256k1fx.TransferOutput{
		Amt:          utxoAmount,
		OutputOwners: outputOwners,
	}
	utxo := avax.UTXO{
		UTXOID: utxoID,
		Asset:  asset,
		Out:    &utxoOut,
	}

	// Input spending the UTXO
	inputSigners := secp256k1fx.Input{
		SigIndices: []uint32{0},
	}
	fxInput := secp256k1fx.TransferInput{
		Amt:   utxoAmount,
		Input: inputSigners,
	}
	input := avax.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In:     &fxInput,
	}

	// Output produced by BaseTx
	fxOutput := secp256k1fx.TransferOutput{
		Amt:          100,
		OutputOwners: outputOwners,
	}
	output := avax.TransferableOutput{
		Asset: asset,
		Out:   &fxOutput,
	}

	// BaseTx
	baseTx := avax.BaseTx{
		Outs: []*avax.TransferableOutput{
			&output,
		},
		Ins: []*avax.TransferableInput{
			&input,
		},
	}

	exportTx := txs.ExportTx{
		BaseTx: txs.BaseTx{
			BaseTx: baseTx,
		},
		DestinationChain: ctx.CChainID,
	}

	typeToFxIndex := make(map[reflect.Type]int)
	secpFx := &secp256k1fx.Fx{}
	parser, err := txs.NewCustomParser(
		typeToFxIndex,
		new(mockable.Clock),
		logging.NoWarn{},
		[]fxs.Fx{
			secpFx,
		},
	)
	require.NoError(err)

	codec := parser.Codec()
	backend := &Backend{
		Ctx:    ctx,
		Config: &feeConfig,
		Fxs: []*fxs.ParsedFx{
			{
				ID: secp256k1fx.ID,
				Fx: secpFx,
			},
		},
		TypeToFxIndex: typeToFxIndex,
		Codec:         codec,
		FeeAssetID:    feeAssetID,
		Bootstrapped:  true,
	}
	require.NoError(secpFx.Bootstrapped())

	unsignedCreateAssetTx := txs.CreateAssetTx{
		States: []*txs.InitialState{{
			FxIndex: 0,
		}},
	}
	createAssetTx := txs.Tx{
		Unsigned: &unsignedCreateAssetTx,
	}

	chain := state.NewMockChain(ctrl)
	chain.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
	chain.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
	chain.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
	chain.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).Times(2)

	tx := &txs.Tx{
		Unsigned: &exportTx,
	}
	require.NoError(tx.SignSECP256K1Fx(
		codec,
		[][]*secp256k1.PrivateKey{
			{keys[0]},
		},
	))

	feeCalc, err := state.PickFeeCalculator(&feeConfig, parser.Codec(), chain, chain.GetTimestamp())
	require.NoError(err)

	err = tx.Unsigned.Visit(&SemanticVerifier{
		Backend:       backend,
		FeeCalculator: feeCalc,
		State:         chain,
		Tx:            tx,
	})
	require.ErrorIs(err, verify.ErrMismatchedSubnetIDs)
}

func TestSemanticVerifierImportTx(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.XChainID)

	m := atomic.NewMemory(prefixdb.New([]byte{0}, memdb.New()))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	typeToFxIndex := make(map[reflect.Type]int)
	fx := &secp256k1fx.Fx{}
	parser, err := txs.NewCustomParser(
		typeToFxIndex,
		new(mockable.Clock),
		logging.NoWarn{},
		[]fxs.Fx{
			fx,
		},
	)
	require.NoError(t, err)
	codec := parser.Codec()

	// UTXOs to be spent
	utxoAmount := 100 + feeConfig.TxFee
	inputTxID := ids.GenerateTestID()
	utxoID := avax.UTXOID{
		TxID:        inputTxID,
		OutputIndex: 0,
	}

	feeAssetID := ids.GenerateTestID()
	asset := avax.Asset{
		ID: feeAssetID,
	}
	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
	}
	utxoOut := secp256k1fx.TransferOutput{
		Amt:          utxoAmount,
		OutputOwners: outputOwners,
	}
	utxo := avax.UTXO{
		UTXOID: utxoID,
		Asset:  asset,
		Out:    &utxoOut,
	}

	// Input spending the UTXO
	inputSigners := secp256k1fx.Input{
		SigIndices: []uint32{0},
	}
	fxInput := secp256k1fx.TransferInput{
		Amt:   utxoAmount,
		Input: inputSigners,
	}
	input := avax.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In:     &fxInput,
	}

	// Output produced by BaseTx
	fxOutput := secp256k1fx.TransferOutput{
		Amt:          100,
		OutputOwners: outputOwners,
	}
	output := avax.TransferableOutput{
		Asset: asset,
		Out:   &fxOutput,
	}

	baseTx := avax.BaseTx{
		NetworkID:    constants.UnitTestID,
		BlockchainID: ctx.ChainID,
		Outs: []*avax.TransferableOutput{
			&output,
		},
		// no inputs here, only imported ones
	}

	importedInput := avax.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In: &secp256k1fx.TransferInput{
			Amt: utxoAmount,
			Input: secp256k1fx.Input{
				SigIndices: []uint32{0},
			},
		},
	}
	unsignedImportTx := txs.ImportTx{
		BaseTx: txs.BaseTx{
			BaseTx: baseTx,
		},
		SourceChain: ctx.CChainID,
		ImportedIns: []*avax.TransferableInput{
			&importedInput,
		},
	}
	importTx := &txs.Tx{
		Unsigned: &unsignedImportTx,
	}
	require.NoError(t, importTx.SignSECP256K1Fx(
		codec,
		[][]*secp256k1.PrivateKey{
			{keys[0]},
			{keys[0]},
		},
	))

	backend := &Backend{
		Ctx:    ctx,
		Config: &feeConfig,
		Fxs: []*fxs.ParsedFx{
			{
				ID: secp256k1fx.ID,
				Fx: fx,
			},
		},
		TypeToFxIndex: typeToFxIndex,
		Codec:         codec,
		FeeAssetID:    feeAssetID,
		Bootstrapped:  true,
	}
	require.NoError(t, fx.Bootstrapped())

	utxoBytes, err := codec.Marshal(txs.CodecVersion, utxo)
	require.NoError(t, err)

	peerSharedMemory := m.NewSharedMemory(ctx.CChainID)
	inputID := utxo.InputID()
	require.NoError(t, peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			keys[0].PublicKey().Address().Bytes(),
		},
	}}}}))

	unsignedCreateAssetTx := txs.CreateAssetTx{
		States: []*txs.InitialState{{
			FxIndex: 0,
		}},
	}
	createAssetTx := txs.Tx{
		Unsigned: &unsignedCreateAssetTx,
	}
	tests := []struct {
		name        string
		stateFunc   func(*gomock.Controller) state.Chain
		txFunc      func(*require.Assertions) *txs.Tx
		expectedErr error
	}{
		{
			name: "valid",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil).AnyTimes()
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).AnyTimes()
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				return importTx
			},
			expectedErr: nil,
		},
		{
			name: "invalid output",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				output := output
				output.Out = &secp256k1fx.TransferOutput{
					Amt:          0,
					OutputOwners: outputOwners,
				}

				utx := unsignedImportTx
				utx.Outs = []*avax.TransferableOutput{
					&output,
				}

				tx := &txs.Tx{Unsigned: &utx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			expectedErr: secp256k1fx.ErrNoValueOutput,
		},
		{
			name: "unsorted outputs",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				output0 := output
				output0.Out = &secp256k1fx.TransferOutput{
					Amt:          1,
					OutputOwners: outputOwners,
				}

				output1 := output
				output1.Out = &secp256k1fx.TransferOutput{
					Amt:          2,
					OutputOwners: outputOwners,
				}

				outputs := []*avax.TransferableOutput{
					&output0,
					&output1,
				}
				avax.SortTransferableOutputs(outputs, codec)
				outputs[0], outputs[1] = outputs[1], outputs[0]

				utx := unsignedImportTx
				utx.Outs = outputs
				tx := &txs.Tx{Unsigned: &utx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			expectedErr: avax.ErrOutputsNotSorted,
		},
		{
			name: "invalid input",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input := input
				input.In = &secp256k1fx.TransferInput{
					Amt:   0,
					Input: inputSigners,
				}

				utx := unsignedImportTx
				utx.Ins = []*avax.TransferableInput{
					&input,
				}
				tx := &txs.Tx{Unsigned: &utx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			expectedErr: secp256k1fx.ErrNoValueInput,
		},
		{
			name: "duplicate inputs",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				utx := unsignedImportTx
				utx.Ins = []*avax.TransferableInput{
					&input,
					&input,
				}
				tx := &txs.Tx{Unsigned: &utx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
						{keys[0]},
						{keys[0]},
					}))
				return tx
			},
			expectedErr: avax.ErrInputsNotSortedUnique,
		},
		{
			name: "duplicate imported inputs",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				utx := unsignedImportTx
				utx.ImportedIns = []*avax.TransferableInput{
					&input,
					&input,
				}
				tx := &txs.Tx{Unsigned: &utx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
						{keys[0]},
					}))
				return tx
			},
			expectedErr: avax.ErrInputsNotSortedUnique,
		},
		{
			name: "input overflow",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input0 := input
				input0.In = &secp256k1fx.TransferInput{
					Amt:   1,
					Input: inputSigners,
				}

				input1 := input
				input1.UTXOID.OutputIndex++
				input1.In = &secp256k1fx.TransferInput{
					Amt:   math.MaxUint64,
					Input: inputSigners,
				}

				utx := unsignedImportTx
				utx.Ins = []*avax.TransferableInput{
					&input0,
					&input1,
				}
				avax.SortTransferableInputsWithSigners(utx.Ins, make([][]*secp256k1.PrivateKey, 2))
				tx := &txs.Tx{Unsigned: &utx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
						{keys[0]},
					}))
				return tx
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "output overflow",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				output := output
				output.Out = &secp256k1fx.TransferOutput{
					Amt:          math.MaxUint64,
					OutputOwners: outputOwners,
				}

				outputs := []*avax.TransferableOutput{
					&output,
				}
				avax.SortTransferableOutputs(outputs, codec)

				utx := unsignedImportTx
				utx.Outs = outputs
				tx := &txs.Tx{Unsigned: &utx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
						{keys[0]},
					}))
				return tx
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "insufficient funds",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				input := input
				input.In = &secp256k1fx.TransferInput{
					Amt:   1,
					Input: inputSigners,
				}

				utx := unsignedImportTx
				utx.ImportedIns = []*avax.TransferableInput{
					&input,
				}
				tx := &txs.Tx{Unsigned: &utx}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
						{keys[0]},
					}))
				return tx
			},
			expectedErr: avax.ErrInsufficientFunds,
		},
		{
			name: "not allowed input feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil
				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil).AnyTimes()
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).AnyTimes()
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				return importTx
			},
			expectedErr: errIncompatibleFx,
		},
		{
			name: "invalid signature",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil).AnyTimes()
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).AnyTimes()
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &unsignedImportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[1]},
					},
				))
				return tx
			},
			expectedErr: secp256k1fx.ErrWrongSig,
		},
		{
			name: "not allowed output feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil
				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).AnyTimes()
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				importTx := unsignedImportTx
				importTx.Ins = nil
				importTx.ImportedIns = []*avax.TransferableInput{
					&input,
				}
				tx := &txs.Tx{
					Unsigned: &importTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					nil,
				))
				return tx
			},
			expectedErr: errIncompatibleFx,
		},
		{
			name: "unknown asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil).AnyTimes()
				state.EXPECT().GetTx(asset.ID).Return(nil, database.ErrNotFound)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				return importTx
			},
			expectedErr: database.ErrNotFound,
		},
		{
			name: "not an asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				tx := txs.Tx{
					Unsigned: &txs.BaseTx{
						BaseTx: baseTx,
					},
				}
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil).AnyTimes()
				state.EXPECT().GetTx(asset.ID).Return(&tx, nil)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				return importTx
			},
			expectedErr: errNotAnAsset,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			chain := test.stateFunc(ctrl)
			tx := test.txFunc(require)

			feeCalc, err := state.PickFeeCalculator(&feeConfig, parser.Codec(), chain, chain.GetTimestamp())
			require.NoError(err)

			err = tx.Unsigned.Visit(&SemanticVerifier{
				Backend:       backend,
				FeeCalculator: feeCalc,
				State:         chain,
				Tx:            tx,
			})
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

func TestSemanticVerifierOperationTx(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.XChainID)

	var (
		secpFx     = &secp256k1fx.Fx{}
		nftFx      = &nftfx.Fx{}
		propertyFx = &propertyfx.Fx{}
	)

	typeToFxIndex := make(map[reflect.Type]int)
	parser, err := txs.NewCustomParser(
		typeToFxIndex,
		new(mockable.Clock),
		logging.NoWarn{},
		[]fxs.Fx{
			secpFx,
			nftFx,
			propertyFx,
		},
	)

	require.NoError(t, err)
	codec := parser.Codec()

	feeAssetID := ids.GenerateTestID()
	feeAsset := avax.Asset{
		ID: feeAssetID,
	}

	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[0].Address(),
		},
	}

	utxoAmount := 20 * units.KiloAvax
	utxoID := avax.UTXOID{
		TxID:        ids.GenerateTestID(),
		OutputIndex: 1,
	}
	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  feeAsset,
		Out: &secp256k1fx.TransferOutput{
			Amt:          20 * units.KiloAvax,
			OutputOwners: outputOwners,
		},
	}

	opUTXOID := avax.UTXOID{
		TxID:        ids.GenerateTestID(),
		OutputIndex: 1,
	}
	opUTXO := &avax.UTXO{
		UTXOID: opUTXOID,
		Asset:  avax.Asset{ID: assetID},
		Out: &secp256k1fx.MintOutput{
			OutputOwners: outputOwners,
		},
	}

	unsignedCreateAssetTx := txs.CreateAssetTx{
		States: []*txs.InitialState{{
			FxIndex: 0,
		}},
	}
	createAssetTx := &txs.Tx{
		Unsigned: &unsignedCreateAssetTx,
	}

	opTxInSigner := secp256k1fx.Input{
		SigIndices: []uint32{
			0,
		},
	}
	opTxIn := avax.TransferableInput{
		UTXOID: utxoID,
		Asset:  feeAsset,
		In: &secp256k1fx.TransferInput{
			Amt:   utxoAmount,
			Input: opTxInSigner,
		},
	}
	opTxOut := avax.TransferableOutput{
		Asset: avax.Asset{ID: feeAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          units.NanoAvax,
			OutputOwners: outputOwners,
		},
	}
	unsignedOperationTx := txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins:          []*avax.TransferableInput{&opTxIn},
			Outs:         []*avax.TransferableOutput{&opTxOut},
		}},
		Ops: []*txs.Operation{{
			Asset: avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{
				&opUTXOID,
			},
			Op: &secp256k1fx.MintOperation{
				MintInput: secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
				MintOutput: secp256k1fx.MintOutput{
					OutputOwners: outputOwners,
				},
				TransferOutput: secp256k1fx.TransferOutput{
					Amt:          1,
					OutputOwners: outputOwners,
				},
			},
		}},
	}

	operationTx := txs.Tx{Unsigned: &unsignedOperationTx}
	require.NoError(t, operationTx.SignSECP256K1Fx(
		codec,
		[][]*secp256k1.PrivateKey{
			{keys[0]},
			{keys[0]},
		},
	))

	backend := &Backend{
		Ctx:    ctx,
		Config: &feeConfig,
		Fxs: []*fxs.ParsedFx{
			{
				ID: secp256k1fx.ID,
				Fx: secpFx,
			},
			{
				ID: nftfx.ID,
				Fx: nftFx,
			},
			{
				ID: propertyfx.ID,
				Fx: propertyFx,
			},
		},
		Codec:         codec,
		TypeToFxIndex: typeToFxIndex,
		FeeAssetID:    feeAssetID,
	}
	require.NoError(t, secpFx.Bootstrapped())
	require.NoError(t, nftFx.Bootstrapped())
	require.NoError(t, propertyFx.Bootstrapped())

	tests := []struct {
		name        string
		stateFunc   func(*gomock.Controller) state.Chain
		txFunc      func(*require.Assertions) *txs.Tx
		expectedErr error
	}{
		{
			name: "valid",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(utxo, nil).AnyTimes()
				state.EXPECT().GetUTXO(opUTXO.InputID()).Return(opUTXO, nil).AnyTimes()
				state.EXPECT().GetTx(feeAssetID).Return(createAssetTx, nil).AnyTimes()
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				return &operationTx
			},
			expectedErr: nil,
		},
		{
			name: "invalid output",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				output := opTxOut
				output.Out = &secp256k1fx.TransferOutput{
					Amt:          0,
					OutputOwners: outputOwners,
				}

				unsignedTx := unsignedOperationTx
				unsignedTx.Outs = []*avax.TransferableOutput{
					&output,
				}
				return &txs.Tx{
					Unsigned: &unsignedTx,
					Creds:    operationTx.Creds,
				}
			},
			expectedErr: secp256k1fx.ErrNoValueOutput,
		},
		{
			name: "unsorted outputs",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				output0 := opTxOut
				output0.Out = &secp256k1fx.TransferOutput{
					Amt:          1,
					OutputOwners: outputOwners,
				}

				output1 := opTxOut
				output1.Out = &secp256k1fx.TransferOutput{
					Amt:          2,
					OutputOwners: outputOwners,
				}

				outputs := []*avax.TransferableOutput{
					&output0,
					&output1,
				}
				avax.SortTransferableOutputs(outputs, codec)
				outputs[0], outputs[1] = outputs[1], outputs[0]

				unsignedTx := unsignedOperationTx
				unsignedTx.Outs = outputs
				return &txs.Tx{
					Unsigned: &unsignedTx,
					Creds:    operationTx.Creds,
				}
			},
			expectedErr: avax.ErrOutputsNotSorted,
		},
		{
			name: "invalid input",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				input := opTxIn
				input.In = &secp256k1fx.TransferInput{
					Amt:   0,
					Input: opTxInSigner,
				}

				tx := unsignedOperationTx
				tx.Ins = []*avax.TransferableInput{
					&input,
				}
				return &txs.Tx{
					Unsigned: &tx,
					Creds:    operationTx.Creds,
				}
			},
			expectedErr: secp256k1fx.ErrNoValueInput,
		},
		{
			name: "duplicate inputs",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				unsignedTx := unsignedOperationTx
				unsignedTx.Ins = []*avax.TransferableInput{
					&opTxIn,
					&opTxIn,
				}

				tx := &txs.Tx{Unsigned: &unsignedTx}
				require.NoError(t, tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
						{keys[0]},
					}))

				return tx
			},
			expectedErr: avax.ErrInputsNotSortedUnique,
		},
		{
			name: "input overflow",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				input0 := opTxIn
				input0.In = &secp256k1fx.TransferInput{
					Amt:   1,
					Input: opTxInSigner,
				}

				input1 := opTxIn
				input1.UTXOID.OutputIndex++
				input1.In = &secp256k1fx.TransferInput{
					Amt:   math.MaxUint64,
					Input: opTxInSigner,
				}

				unsignedTx := unsignedOperationTx
				unsignedTx.Ins = []*avax.TransferableInput{
					&input0,
					&input1,
				}
				avax.SortTransferableInputsWithSigners(unsignedTx.Ins, make([][]*secp256k1.PrivateKey, 2))

				tx := &txs.Tx{Unsigned: &unsignedTx}
				require.NoError(t, tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
						{keys[0]},
					}))
				return tx
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "output overflow",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				output := opTxOut
				output.Out = &secp256k1fx.TransferOutput{
					Amt:          math.MaxUint64,
					OutputOwners: outputOwners,
				}

				outputs := []*avax.TransferableOutput{
					&output,
				}
				avax.SortTransferableOutputs(outputs, codec)

				unsignedTx := unsignedOperationTx
				unsignedTx.Outs = outputs
				return &txs.Tx{
					Unsigned: &unsignedTx,
					Creds:    operationTx.Creds,
				}
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "insufficient funds",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				input := opTxIn
				input.In = &secp256k1fx.TransferInput{
					Amt:   1,
					Input: opTxInSigner,
				}

				unsignedTx := unsignedOperationTx
				unsignedTx.Ins = []*avax.TransferableInput{
					&input,
				}
				return &txs.Tx{
					Unsigned: &unsignedTx,
					Creds:    operationTx.Creds,
				}
			},
			expectedErr: avax.ErrInsufficientFunds,
		},
		{
			name: "barely sufficient funds",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				barelySufficientUtxo := &avax.UTXO{
					UTXOID: utxoID,
					Asset:  feeAsset,
					Out: &secp256k1fx.TransferOutput{
						Amt:          1801 * units.NanoAvax,
						OutputOwners: outputOwners,
					},
				}

				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(barelySufficientUtxo, nil).AnyTimes()
				state.EXPECT().GetUTXO(opUTXO.InputID()).Return(opUTXO, nil).AnyTimes()
				state.EXPECT().GetTx(feeAssetID).Return(createAssetTx, nil).AnyTimes()
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				input := opTxIn
				input.In = &secp256k1fx.TransferInput{
					Amt:   1801 * units.NanoAvax,
					Input: opTxInSigner,
				}

				unsignedTx := unsignedOperationTx
				unsignedTx.Ins = []*avax.TransferableInput{
					&input,
				}
				return &txs.Tx{
					Unsigned: &unsignedTx,
					Creds:    operationTx.Creds,
				}
			},
			expectedErr: nil,
		},
		{
			name: "barely insufficient funds",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetTimestamp().Return(time.Now().Truncate(time.Second)).Times(2)
				state.EXPECT().GetCurrentGasCap().Return(commonfee.Gas(1_000_000), nil)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				input := opTxIn
				input.In = &secp256k1fx.TransferInput{
					Amt:   1800 * units.NanoAvax,
					Input: opTxInSigner,
				}

				unsignedTx := unsignedOperationTx
				unsignedTx.Ins = []*avax.TransferableInput{
					&input,
				}
				return &txs.Tx{
					Unsigned: &unsignedTx,
					Creds:    operationTx.Creds,
				}
			},
			expectedErr: avax.ErrInsufficientFunds,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			chain := test.stateFunc(ctrl)
			tx := test.txFunc(require)

			feeCalc, err := state.PickFeeCalculator(&feeConfig, parser.Codec(), chain, chain.GetTimestamp())
			require.NoError(err)

			err = tx.Unsigned.Visit(&SemanticVerifier{
				Backend:       backend,
				FeeCalculator: feeCalc,
				State:         chain,
				Tx:            tx,
			})
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

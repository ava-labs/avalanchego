// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/states"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestSemanticVerifierBaseTx(t *testing.T) {
	ctx := newContext(t)

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
	txID := ids.GenerateTestID()
	utxoID := avax.UTXOID{
		TxID:        txID,
		OutputIndex: 2,
	}
	asset := avax.Asset{
		ID: ids.GenerateTestID(),
	}
	inputSigner := secp256k1fx.Input{
		SigIndices: []uint32{
			0,
		},
	}
	fxInput := secp256k1fx.TransferInput{
		Amt:   12345,
		Input: inputSigner,
	}
	input := avax.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In:     &fxInput,
	}
	baseTx := txs.BaseTx{
		BaseTx: avax.BaseTx{
			Ins: []*avax.TransferableInput{
				&input,
			},
		},
	}

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
		FeeAssetID:    ids.GenerateTestID(),
		Bootstrapped:  true,
	}
	require.NoError(t, secpFx.Bootstrapped())

	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[0].Address(),
		},
	}
	output := secp256k1fx.TransferOutput{
		Amt:          12345,
		OutputOwners: outputOwners,
	}
	utxo := avax.UTXO{
		UTXOID: utxoID,
		Asset:  asset,
		Out:    &output,
	}
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
		stateFunc func(*gomock.Controller) states.Chain
		txFunc    func(*require.Assertions) *txs.Tx
		err       error
	}{
		{
			name: "valid",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: nil,
		},
		{
			name: "assetID mismatch",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				utxo := utxo
				utxo.Asset.ID = ids.GenerateTestID()

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: errAssetIDMismatch,
		},
		{
			name: "not allowed input feature extension",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "invalid signature",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[1]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: secp256k1fx.ErrWrongSig,
		},
		{
			name: "missing UTXO",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				state.EXPECT().GetUTXOFromID(&utxoID).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "invalid UTXO amount",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				output := output
				output.Amt--

				utxo := utxo
				utxo.Out = &output

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: secp256k1fx.ErrMismatchedAmounts,
		},
		{
			name: "not allowed output feature extension",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				baseTx := baseTx
				baseTx.Ins = nil
				baseTx.Outs = []*avax.TransferableOutput{
					{
						Asset: asset,
						Out:   &output,
					},
				}
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{},
				)
				require.NoError(err)
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "unknown asset",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "not an asset",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				tx := txs.Tx{
					Unsigned: &baseTx,
				}

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&tx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: errNotAnAsset,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			state := test.stateFunc(ctrl)
			tx := test.txFunc(require)

			err = tx.Unsigned.Visit(&SemanticVerifier{
				Backend: backend,
				State:   state,
				Tx:      tx,
			})
			require.ErrorIs(err, test.err)
		})
	}
}

func TestSemanticVerifierExportTx(t *testing.T) {
	ctx := newContext(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	validatorState := validators.NewMockState(ctrl)
	validatorState.EXPECT().GetSubnetID(gomock.Any(), ctx.CChainID).AnyTimes().Return(ctx.SubnetID, nil)
	ctx.ValidatorState = validatorState

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
	txID := ids.GenerateTestID()
	utxoID := avax.UTXOID{
		TxID:        txID,
		OutputIndex: 2,
	}
	asset := avax.Asset{
		ID: ids.GenerateTestID(),
	}
	inputSigner := secp256k1fx.Input{
		SigIndices: []uint32{
			0,
		},
	}
	fxInput := secp256k1fx.TransferInput{
		Amt:   12345,
		Input: inputSigner,
	}
	input := avax.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In:     &fxInput,
	}
	baseTx := txs.BaseTx{
		BaseTx: avax.BaseTx{
			Ins: []*avax.TransferableInput{
				&input,
			},
		},
	}
	exportTx := txs.ExportTx{
		BaseTx:           baseTx,
		DestinationChain: ctx.CChainID,
	}

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
		FeeAssetID:    ids.GenerateTestID(),
		Bootstrapped:  true,
	}
	require.NoError(t, secpFx.Bootstrapped())

	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[0].Address(),
		},
	}
	output := secp256k1fx.TransferOutput{
		Amt:          12345,
		OutputOwners: outputOwners,
	}
	utxo := avax.UTXO{
		UTXOID: utxoID,
		Asset:  asset,
		Out:    &output,
	}
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
		stateFunc func(*gomock.Controller) states.Chain
		txFunc    func(*require.Assertions) *txs.Tx
		err       error
	}{
		{
			name: "valid",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: nil,
		},
		{
			name: "assetID mismatch",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				utxo := utxo
				utxo.Asset.ID = ids.GenerateTestID()

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: errAssetIDMismatch,
		},
		{
			name: "not allowed input feature extension",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "invalid signature",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[1]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: secp256k1fx.ErrWrongSig,
		},
		{
			name: "missing UTXO",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				state.EXPECT().GetUTXOFromID(&utxoID).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "invalid UTXO amount",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				output := output
				output.Amt--

				utxo := utxo
				utxo.Out = &output

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: secp256k1fx.ErrMismatchedAmounts,
		},
		{
			name: "not allowed output feature extension",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				exportTx := exportTx
				exportTx.Ins = nil
				exportTx.ExportedOuts = []*avax.TransferableOutput{
					{
						Asset: asset,
						Out:   &output,
					},
				}
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{},
				)
				require.NoError(err)
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "unknown asset",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "not an asset",
			stateFunc: func(ctrl *gomock.Controller) states.Chain {
				state := states.NewMockChain(ctrl)

				tx := txs.Tx{
					Unsigned: &baseTx,
				}

				state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&tx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				err := tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				)
				require.NoError(err)
				return tx
			},
			err: errNotAnAsset,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			state := test.stateFunc(ctrl)
			tx := test.txFunc(require)

			err = tx.Unsigned.Visit(&SemanticVerifier{
				Backend: backend,
				State:   state,
				Tx:      tx,
			})
			require.ErrorIs(err, test.err)
		})
	}
}

func TestSemanticVerifierExportTxDifferentSubnet(t *testing.T) {
	require := require.New(t)

	ctx := newContext(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	validatorState := validators.NewMockState(ctrl)
	validatorState.EXPECT().GetSubnetID(gomock.Any(), ctx.CChainID).AnyTimes().Return(ids.GenerateTestID(), nil)
	ctx.ValidatorState = validatorState

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
	txID := ids.GenerateTestID()
	utxoID := avax.UTXOID{
		TxID:        txID,
		OutputIndex: 2,
	}
	asset := avax.Asset{
		ID: ids.GenerateTestID(),
	}
	inputSigner := secp256k1fx.Input{
		SigIndices: []uint32{
			0,
		},
	}
	fxInput := secp256k1fx.TransferInput{
		Amt:   12345,
		Input: inputSigner,
	}
	input := avax.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In:     &fxInput,
	}
	baseTx := txs.BaseTx{
		BaseTx: avax.BaseTx{
			Ins: []*avax.TransferableInput{
				&input,
			},
		},
	}
	exportTx := txs.ExportTx{
		BaseTx:           baseTx,
		DestinationChain: ctx.CChainID,
	}

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
		FeeAssetID:    ids.GenerateTestID(),
		Bootstrapped:  true,
	}
	require.NoError(secpFx.Bootstrapped())

	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[0].Address(),
		},
	}
	output := secp256k1fx.TransferOutput{
		Amt:          12345,
		OutputOwners: outputOwners,
	}
	utxo := avax.UTXO{
		UTXOID: utxoID,
		Asset:  asset,
		Out:    &output,
	}
	unsignedCreateAssetTx := txs.CreateAssetTx{
		States: []*txs.InitialState{{
			FxIndex: 0,
		}},
	}
	createAssetTx := txs.Tx{
		Unsigned: &unsignedCreateAssetTx,
	}

	state := states.NewMockChain(ctrl)

	state.EXPECT().GetUTXOFromID(&utxoID).Return(&utxo, nil)
	state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

	tx := &txs.Tx{
		Unsigned: &exportTx,
	}
	err = tx.SignSECP256K1Fx(
		codec,
		[][]*secp256k1.PrivateKey{
			{keys[0]},
		},
	)
	require.NoError(err)

	err = tx.Unsigned.Visit(&SemanticVerifier{
		Backend: backend,
		State:   state,
		Tx:      tx,
	})
	require.ErrorIs(err, verify.ErrMismatchedSubnetIDs)
}

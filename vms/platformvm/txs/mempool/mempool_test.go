// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"math"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	avaxAssetID = ids.ID{1, 2, 3}
	validTx     = txs.BaseTx{
		BaseTx: avax.BaseTx{
			Ins: []*avax.TransferableInput{
				newAVAXInput(ids.GenerateTestID(), 2),
			},
			Outs: []*avax.TransferableOutput{
				newAVAXOutput(1),
			},
		},
	}
	invalidTxInputOverflow = txs.BaseTx{
		BaseTx: avax.BaseTx{
			Ins: []*avax.TransferableInput{
				newAVAXInput(ids.GenerateTestID(), math.MaxUint64),
				newAVAXInput(ids.GenerateTestID(), math.MaxUint64),
			},
		},
	}
	invalidTxOutputOverflow = txs.BaseTx{
		BaseTx: avax.BaseTx{
			Ins: []*avax.TransferableInput{
				newAVAXInput(ids.GenerateTestID(), 10),
			},
			Outs: []*avax.TransferableOutput{
				newAVAXOutput(math.MaxUint64),
				newAVAXOutput(math.MaxUint64),
			},
		},
	}
)

func newAVAXInput(txID ids.ID, amount uint64) *avax.TransferableInput {
	return &avax.TransferableInput{
		UTXOID: avax.UTXOID{
			TxID: txID,
		},
		Asset: avax.Asset{
			ID: avaxAssetID,
		},
		In: &secp256k1fx.TransferInput{
			Amt: amount,
		},
	}
}

func newAVAXOutput(amount uint64) *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Asset: avax.Asset{
			ID: avaxAssetID,
		},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
		},
	}
}

func newTx(
	txID ids.ID,
	inputs []*avax.TransferableInput,
	outputAmount uint64,
) *txs.Tx {
	tx := avax.BaseTx{
		Ins: inputs,
	}

	if outputAmount > 0 {
		tx.Outs = []*avax.TransferableOutput{
			newAVAXOutput(outputAmount),
		}
	}

	return &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: tx,
		},
		TxID: txID,
	}
}

// Txs should be prioritized by highest gas price during after Etna
func TestMempoolOrdering(t *testing.T) {
	require := require.New(t)

	weights := gas.Dimensions{gas.Bandwidth: 1}
	linearCodec := linearcodec.NewDefault()
	require.NoError(linearCodec.RegisterType(&secp256k1fx.TransferOutput{}))
	codecManager := codec.NewDefaultManager()
	require.NoError(codecManager.RegisterCodec(0, linearCodec))

	m, err := New(
		"",
		weights,
		1_000_000,
		avaxAssetID,
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	lowTx := newTx(
		ids.GenerateTestID(),
		[]*avax.TransferableInput{newAVAXInput(ids.GenerateTestID(), 5)},
		4,
	)
	require.NoError(m.Add(lowTx))

	highTx := newTx(
		ids.GenerateTestID(),
		[]*avax.TransferableInput{newAVAXInput(ids.GenerateTestID(), 5)},
		1,
	)
	require.NoError(m.Add(highTx))

	gotTx, ok := m.Peek()
	require.True(ok)
	require.Equal(highTx, gotTx)
	m.Remove(gotTx.ID())

	gotTx, ok = m.Peek()
	require.True(ok)
	require.Equal(lowTx, gotTx)
}

// TODO need to test happy case for everything too (??)
func TestMempoolAdd(t *testing.T) {
	tests := []struct {
		name           string
		weights        gas.Dimensions
		maxGasCapacity gas.Gas
		prevTxs        []*txs.Tx
		tx             *txs.Tx
		wantErr        error
		wantTxIDs      []ids.ID
	}{
		{
			name: "dropped - no input AVAX",
			tx: newTx(
				ids.GenerateTestID(),
				[]*avax.TransferableInput{},
				0,
			),
			wantErr: errMissingConsumedAVAX,
		},
		// TODO do this for each tx type?
		{
			name:    "dropped - no gas",
			weights: gas.Dimensions{},
			tx:      &txs.Tx{Unsigned: &validTx},
			wantErr: errNoGasUsed,
		},
		{
			name:           "dropped - AddValidatorTx consumed AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.AddValidatorTx{
					BaseTx: invalidTxInputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - AddValidatorTx produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.AddValidatorTx{
					BaseTx: invalidTxOutputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - AddValidatorTx staked output overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.AddValidatorTx{
					BaseTx: validTx,
					StakeOuts: []*avax.TransferableOutput{
						newAVAXOutput(math.MaxUint64),
						newAVAXOutput(math.MaxUint64),
					},
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - AddValidatorTx total produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.AddValidatorTx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								newAVAXInput(ids.GenerateTestID(), 10),
							},
							Outs: []*avax.TransferableOutput{
								newAVAXOutput(math.MaxUint64),
							},
						},
					},
					StakeOuts: []*avax.TransferableOutput{
						newAVAXOutput(math.MaxUint64),
					},
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - AddSubnetValidatorTx consumed AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.AddValidatorTx{
					BaseTx: invalidTxInputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - AddSubnetValidatorTx produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.AddValidatorTx{
					BaseTx: invalidTxOutputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - AddDelegatorTx consumed AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.AddDelegatorTx{
					BaseTx: invalidTxInputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - AddDelegatorTx produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.AddDelegatorTx{
					BaseTx: invalidTxOutputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - AddDelegatorTx staked output overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.AddDelegatorTx{
					BaseTx: validTx,
					StakeOuts: []*avax.TransferableOutput{
						newAVAXOutput(math.MaxUint64),
						newAVAXOutput(math.MaxUint64),
					},
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - AddDelegatorTx total produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.AddDelegatorTx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								newAVAXInput(ids.GenerateTestID(), 10),
							},
							Outs: []*avax.TransferableOutput{
								newAVAXOutput(math.MaxUint64),
							},
						},
					},
					StakeOuts: []*avax.TransferableOutput{
						newAVAXOutput(math.MaxUint64),
					},
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - CreateChainTx consumed AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.CreateChainTx{
					BaseTx: invalidTxInputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - CreateChainTx produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.CreateChainTx{
					BaseTx: invalidTxOutputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - CreateSubnetTx consumed AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.CreateSubnetTx{
					BaseTx: invalidTxInputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - CreateSubnetTx produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.CreateSubnetTx{
					BaseTx: invalidTxOutputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - ImportTx consumed AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.ImportTx{
					BaseTx: invalidTxInputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - ImportTx total consumed AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.ImportTx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								newAVAXInput(ids.GenerateTestID(), math.MaxUint64),
							},
						},
					},
					ImportedInputs: []*avax.TransferableInput{
						newAVAXInput(ids.GenerateTestID(), math.MaxUint64),
					},
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - ImportTx produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.ImportTx{
					BaseTx: invalidTxOutputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - ExportTx consumed AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.ExportTx{
					BaseTx: invalidTxInputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - ExportTx produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.ExportTx{
					BaseTx: invalidTxOutputOverflow,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - ExportTx total produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.ExportTx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								newAVAXInput(ids.GenerateTestID(), 1),
							},
							Outs: []*avax.TransferableOutput{
								newAVAXOutput(math.MaxUint64),
							},
						},
					},
					ExportedOutputs: []*avax.TransferableOutput{
						newAVAXOutput(math.MaxUint64),
					},
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - ConvertSubnetToL1Tx consumed AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.ConvertSubnetToL1Tx{
					BaseTx:     invalidTxInputOverflow,
					SubnetAuth: &secp256k1fx.Input{},
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - ConvertSubnetToL1Tx produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.ConvertSubnetToL1Tx{
					BaseTx:     invalidTxOutputOverflow,
					SubnetAuth: &secp256k1fx.Input{},
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - ConvertSubnetToL1Tx validator balance overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.ConvertSubnetToL1Tx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								newAVAXInput(ids.GenerateTestID(), 10),
							},
						},
					},
					SubnetAuth: &secp256k1fx.Input{},
					Validators: []*txs.ConvertSubnetToL1Validator{
						{
							NodeID:  []byte("foo"),
							Weight:  100,
							Balance: math.MaxUint,
						},
						{
							NodeID:  []byte("bar"),
							Weight:  100,
							Balance: math.MaxUint,
						},
					},
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name:           "dropped - ConvertSubnetToL1Tx total produced AVAX overflows",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			tx: &txs.Tx{
				Unsigned: &txs.ConvertSubnetToL1Tx{
					BaseTx: txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								newAVAXInput(ids.GenerateTestID(), 10),
							},
							Outs: []*avax.TransferableOutput{
								newAVAXOutput(math.MaxUint64),
							},
						},
					},
					SubnetAuth: &secp256k1fx.Input{},
					Validators: []*txs.ConvertSubnetToL1Validator{
						{
							NodeID:  []byte("foo"),
							Weight:  100,
							Balance: math.MaxUint,
						},
					},
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name: "dropped - AdvanceTimeTx",
			tx: &txs.Tx{
				Unsigned: &txs.AdvanceTimeTx{},
			},
			wantErr: utxo.ErrUnsupportedTxType,
		},
		{
			name: "dropped - RewardValidatorTx",
			tx: &txs.Tx{
				Unsigned: &txs.RewardValidatorTx{},
			},
			wantErr: utxo.ErrUnsupportedTxType,
		},
		{
			name:           "conflict - lower paying tx is not added",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				newTx(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.Empty, 2)},
					0,
				),
			},
			tx: newTx(
				ids.ID{2},
				[]*avax.TransferableInput{newAVAXInput(ids.Empty, 1)},
				0,
			),
			wantErr: ErrGasCapacityExceeded,
			wantTxIDs: []ids.ID{
				{1},
			},
		},
		{
			name:           "conflict - equal paying tx is not added",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				newTx(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.Empty, 2)},
					0,
				),
			},
			tx: newTx(
				ids.ID{2},
				[]*avax.TransferableInput{newAVAXInput(ids.Empty, 2)},
				0,
			),
			wantErr: ErrGasCapacityExceeded,
			wantTxIDs: []ids.ID{
				{1},
			},
		},
		{
			name:           "conflict - higher paying tx is added",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				newTx(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.Empty, 1)},
					0,
				),
			},
			tx: newTx(
				ids.ID{2},
				[]*avax.TransferableInput{newAVAXInput(ids.Empty, 10)},
				0,
			),
			wantTxIDs: []ids.ID{
				{2},
			},
		},
		{
			name:           "evict - higher paying tx without conflicts is added",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				newTx(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.GenerateTestID(), 5)},
					0,
				),
			},
			tx: newTx(
				ids.ID{2},
				[]*avax.TransferableInput{newAVAXInput(ids.GenerateTestID(), 10)},
				0,
			),
			wantTxIDs: []ids.ID{
				{2},
			},
		},
		{
			name:           "evict - higher paying tx conflicts with multiple txs",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 500,
			prevTxs: []*txs.Tx{
				newTx(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{1, 2, 3}, 1)},
					0,
				),
				newTx(
					ids.ID{2},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{4, 5, 6}, 10)},
					0,
				),
			},
			tx: newTx(
				ids.ID{3},
				[]*avax.TransferableInput{
					newAVAXInput(ids.ID{1, 2, 3}, 2),
					newAVAXInput(ids.ID{4, 5, 6}, 2),
				},
				0,
			),
			wantErr: mempool.ErrConflictsWithOtherTx,
			wantTxIDs: []ids.ID{
				{1},
				{2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			m, err := New(
				"",
				tt.weights,
				tt.maxGasCapacity,
				avaxAssetID,
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			for _, tx := range tt.prevTxs {
				require.NoError(m.Add(tx))
			}

			err = m.Add(tt.tx)
			require.ErrorIs(err, tt.wantErr)

			for _, wantTxID := range tt.wantTxIDs {
				_, ok := m.Get(wantTxID)
				require.True(ok)
			}

			require.Equal(len(tt.wantTxIDs), m.Len())
		})
	}
}

func TestMempool_Remove(t *testing.T) {
	tests := []struct {
		name         string
		txs          []*txs.Tx
		txIDToRemove ids.ID
		wantRemove   bool
	}{
		{
			name:         "remove tx not in mempool - empty",
			txIDToRemove: ids.GenerateTestID(),
		},
		{
			name: "remove tx not in mempool - populated",
			txs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.GenerateTestID(),
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
					TxID: ids.GenerateTestID(),
				},
			},
			txIDToRemove: ids.GenerateTestID(),
		},
		{
			name: "remove tx in mempool",
			txs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.GenerateTestID(),
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
					TxID: ids.ID{1, 2, 3},
				},
			},
			txIDToRemove: ids.ID{1, 2, 3},
			wantRemove:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			m, err := New(
				"",
				gas.Dimensions{1, 1, 1, 1},
				1_000_000,
				avaxAssetID,
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			for _, tx := range tt.txs {
				require.NoError(m.Add(tx))
			}

			m.Remove(tt.txIDToRemove)
			_, gotOk := m.Get(tt.txIDToRemove)
			require.False(gotOk)
		})
	}
}

func TestMempool_RemoveConflicts(t *testing.T) {
	tests := []struct {
		name              string
		txs               []*txs.Tx
		conflictsToRemove set.Set[ids.ID]
		wantTxs           []ids.ID
	}{
		{
			name: "remove conflicts not in mempool - empty",
		},
		{
			name: "remove conflicts not in mempool - populated",
			txs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{1},
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
					TxID: ids.ID{2},
				},
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{3},
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
					TxID: ids.ID{4},
				},
			},
			conflictsToRemove: set.Of(
				ids.ID{1},
				ids.ID{3},
			),
		},
		{
			name: "remove conflicts not in mempool - populated",
			txs: []*txs.Tx{
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{1},
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
					TxID: ids.ID{2},
				},
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{3},
									},
									Asset: avax.Asset{
										ID: avaxAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
					TxID: ids.ID{4},
				},
			},
			conflictsToRemove: set.Of(ids.ID{1}),
			wantTxs:           []ids.ID{{4}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			m, err := New(
				"",
				gas.Dimensions{1, 1, 1, 1},
				1_000_000,
				avaxAssetID,
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			for _, tx := range tt.txs {
				require.NoError(m.Add(tx))
			}

			conflictsToRemove := set.Set[ids.ID]{}
			for conflict := range tt.conflictsToRemove {
				conflictsToRemove.Add(conflict.Prefix(0))
			}

			m.RemoveConflicts(conflictsToRemove)

			require.Equal(len(tt.wantTxs), m.Len())
			for _, wantTxID := range tt.wantTxs {
				_, ok := m.Get(wantTxID)
				require.True(ok)
			}
		})
	}
}

func TestMempool_Drop(t *testing.T) {
	errFoo := errors.New("foo")

	tests := []struct {
		name       string
		droppedTxs map[ids.ID]error
		tx         ids.ID
		wantErr    error
	}{
		{
			name: "tx is dropped",
			droppedTxs: map[ids.ID]error{
				ids.Empty: errFoo,
			},
			tx:      ids.Empty,
			wantErr: errFoo,
		},
		{
			name: "tx not dropped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			m, err := New(
				"",
				gas.Dimensions{1, 1, 1, 1},
				1_000_000,
				ids.Empty,
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			for txID, dropReason := range tt.droppedTxs {
				m.MarkDropped(txID, dropReason)
			}

			err = m.GetDropReason(tt.tx)
			require.ErrorIs(err, tt.wantErr)
		})
	}
}

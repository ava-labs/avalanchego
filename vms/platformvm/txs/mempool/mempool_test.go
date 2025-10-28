// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

func newAVAXInput(txID ids.ID, amount uint64) *avax.TransferableInput {
	return &avax.TransferableInput{
		UTXOID: avax.UTXOID{
			TxID: txID,
		},
		Asset: avax.Asset{
			ID: snowtest.AVAXAssetID,
		},
		In: &secp256k1fx.TransferInput{
			Amt: amount,
		},
	}
}

func newAVAXOutput(amount uint64) *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Asset: avax.Asset{
			ID: snowtest.AVAXAssetID,
		},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
		},
	}
}

func newTx(txID ids.ID) *txs.Tx {
	return newTxWithUTXOs(
		txID,
		[]*avax.TransferableInput{newAVAXInput(ids.GenerateTestID(), 2)},
		1,
	)
}

func newTxWithUTXOs(
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

	m, err := New(
		"",
		weights,
		1_000_000,
		snowtest.AVAXAssetID,
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	lowTx := newTxWithUTXOs(
		ids.GenerateTestID(),
		[]*avax.TransferableInput{newAVAXInput(ids.GenerateTestID(), 5)},
		4,
	)
	require.NoError(m.Add(lowTx))

	highTx := newTxWithUTXOs(
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
			name:    "dropped - no gas",
			weights: gas.Dimensions{},
			tx:      newTx(ids.GenerateTestID()),
			wantErr: errNoGasUsed,
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
			name:           "not enough gas - lower paying tx is not added",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				newTxWithUTXOs(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 2)},
					0,
				),
			},
			tx: newTxWithUTXOs(
				ids.ID{2},
				[]*avax.TransferableInput{newAVAXInput(ids.ID{2}, 1)},
				0,
			),
			wantErr: ErrNotEnoughGas,
			wantTxIDs: []ids.ID{
				{1},
			},
		},
		{
			name:           "not enough gas - equal paying tx is not added",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				newTxWithUTXOs(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 2)},
					0,
				),
			},
			tx: newTxWithUTXOs(
				ids.ID{2},
				[]*avax.TransferableInput{newAVAXInput(ids.ID{2}, 2)},
				0,
			),
			wantErr: ErrNotEnoughGas,
			wantTxIDs: []ids.ID{
				{1},
			},
		},
		{
			name:           "evict - higher paying tx is added",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				newTxWithUTXOs(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 1)},
					0,
				),
			},
			tx: newTxWithUTXOs(
				ids.ID{2},
				[]*avax.TransferableInput{newAVAXInput(ids.ID{2}, 10)},
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
				newTxWithUTXOs(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.GenerateTestID(), 5)},
					0,
				),
			},
			tx: newTxWithUTXOs(
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
				newTxWithUTXOs(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 1)},
					0,
				),
				newTxWithUTXOs(
					ids.ID{2},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{2}, 10)},
					0,
				),
			},
			tx: newTxWithUTXOs(
				ids.ID{3},
				[]*avax.TransferableInput{
					newAVAXInput(ids.ID{1}, 2),
					newAVAXInput(ids.ID{2}, 2),
				},
				0,
			),
			wantErr: txmempool.ErrConflictsWithOtherTx,
			wantTxIDs: []ids.ID{
				{1},
				{2},
			},
		},
		{
			name:           "evict - higher paying tx evicts multiple txs",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 400,
			prevTxs: []*txs.Tx{
				newTxWithUTXOs(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 10)},
					0,
				),
				newTxWithUTXOs(
					ids.ID{2},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{2}, 5)},
					0,
				),
			},
			tx: newTxWithUTXOs(
				ids.ID{3},
				[]*avax.TransferableInput{
					newAVAXInput(ids.ID{3}, 20),
					newAVAXInput(ids.ID{4}, 20),
					newAVAXInput(ids.ID{5}, 20),
				},
				0,
			),
			wantTxIDs: []ids.ID{
				{3},
			},
		},
		{
			name:           "cannot evict - not enough gas capacity",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 400,
			prevTxs: []*txs.Tx{
				newTxWithUTXOs(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 10)},
					0,
				),
				newTxWithUTXOs(
					ids.ID{2},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{2}, 5)},
					0,
				),
			},
			tx: newTxWithUTXOs(
				ids.ID{3},
				[]*avax.TransferableInput{
					newAVAXInput(ids.ID{3}, 20),
					newAVAXInput(ids.ID{4}, 20),
					newAVAXInput(ids.ID{5}, 20),
					newAVAXInput(ids.ID{6}, 20),
				},
				0,
			),
			wantTxIDs: []ids.ID{
				{1},
				{2},
			},
			wantErr: ErrNotEnoughGas,
		},
		{
			name:           "conflict - higher paying tx is dropped",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				newTxWithUTXOs(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 1)},
					0,
				),
			},
			tx: newTxWithUTXOs(
				ids.ID{2},
				[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 10)},
				0,
			),
			wantTxIDs: []ids.ID{
				{1},
			},
			wantErr: txmempool.ErrConflictsWithOtherTx,
		},
		{
			name:           "conflict - equal paying tx is dropped",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				newTxWithUTXOs(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 1)},
					0,
				),
			},
			tx: newTxWithUTXOs(
				ids.ID{2},
				[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 1)},
				0,
			),
			wantTxIDs: []ids.ID{
				{1},
			},
			wantErr: txmempool.ErrConflictsWithOtherTx,
		},
		{
			name:           "conflict - lower paying tx is dropped",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			prevTxs: []*txs.Tx{
				newTxWithUTXOs(
					ids.ID{1},
					[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 10)},
					0,
				),
			},
			tx: newTxWithUTXOs(
				ids.ID{2},
				[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 1)},
				0,
			),
			wantTxIDs: []ids.ID{
				{1},
			},
			wantErr: txmempool.ErrConflictsWithOtherTx,
		},
		{
			name:           "AVAX minted",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			tx: newTxWithUTXOs(
				ids.ID{1},
				[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 1)},
				2,
			),
			wantErr: errAVAXMinted,
		},
		{
			name:           "tx added",
			weights:        gas.Dimensions{1, 1, 1, 1},
			maxGasCapacity: 200,
			tx: newTxWithUTXOs(
				ids.ID{1},
				[]*avax.TransferableInput{newAVAXInput(ids.ID{1}, 1)},
				0,
			),
			wantTxIDs: []ids.ID{
				{1},
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
				snowtest.AVAXAssetID,
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
										ID: snowtest.AVAXAssetID,
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
										ID: snowtest.AVAXAssetID,
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
				snowtest.AVAXAssetID,
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
			name:              "remove conflicts from empty mempool",
			conflictsToRemove: set.Of(ids.ID{1}),
		},
		{
			name: "remove conflicts in mempool",
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
										ID: snowtest.AVAXAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
					TxID: ids.ID{1},
				},
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{2},
									},
									Asset: avax.Asset{
										ID: snowtest.AVAXAssetID,
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
			},
			conflictsToRemove: set.Of(
				ids.ID{1},
			),
			wantTxs: []ids.ID{{2}},
		},
		{
			name: "remove multiple conflicts in mempool",
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
										ID: snowtest.AVAXAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
					TxID: ids.ID{1},
				},
				{
					Unsigned: &txs.BaseTx{
						BaseTx: avax.BaseTx{
							Ins: []*avax.TransferableInput{
								{
									UTXOID: avax.UTXOID{
										TxID: ids.ID{2},
									},
									Asset: avax.Asset{
										ID: snowtest.AVAXAssetID,
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
			},
			conflictsToRemove: set.Of(
				ids.ID{1},
				ids.ID{2},
			),
		},
		{
			name: "remove conflicts not in mempool",
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
										ID: snowtest.AVAXAssetID,
									},
									In: &secp256k1fx.TransferInput{
										Amt: 1,
									},
								},
							},
						},
					},
					TxID: ids.ID{1},
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
										ID: snowtest.AVAXAssetID,
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
			},
			conflictsToRemove: set.Of(ids.ID{123}),
			wantTxs:           []ids.ID{{1}, {2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			m, err := New(
				"",
				gas.Dimensions{1, 1, 1, 1},
				1_000_000,
				snowtest.AVAXAssetID,
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
		addTxs     []ids.ID
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
			name: "tx dropped then added",
			droppedTxs: map[ids.ID]error{
				ids.Empty: errFoo,
			},
			addTxs: []ids.ID{ids.Empty},
			tx:     ids.Empty,
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
				snowtest.AVAXAssetID,
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			for txID, dropReason := range tt.droppedTxs {
				m.MarkDropped(txID, dropReason)
			}

			for _, txID := range tt.addTxs {
				require.NoError(m.Add(newTx(txID)))
			}

			err = m.GetDropReason(tt.tx)
			require.ErrorIs(err, tt.wantErr)
		})
	}
}

func TestMempool_WaitForEvent(t *testing.T) {
	require := require.New(t)

	m, err := New(
		"",
		gas.Dimensions{1, 1, 1, 1},
		1_000_000,
		snowtest.AVAXAssetID,
		prometheus.NewRegistry(),
	)
	require.NoError(err)

	eg := &errgroup.Group{}
	eg.Go(func() error {
		tx := newTxWithUTXOs(
			ids.GenerateTestID(),
			[]*avax.TransferableInput{newAVAXInput(ids.GenerateTestID(), 5)},
			1,
		)

		return m.Add(tx)
	})

	gotMsg, err := m.WaitForEvent(t.Context())
	require.NoError(err)
	require.Equal(common.PendingTxs, gotMsg)

	require.NoError(eg.Wait())
}

func TestMempool_Iterate(t *testing.T) {
	tests := []struct {
		name    string
		txs     []*txs.Tx
		wantTxs []ids.ID
	}{
		{
			name: "no txs",
		},
		{
			name: "one tx",
			txs: []*txs.Tx{
				newTx(ids.ID{1}),
			},
			wantTxs: []ids.ID{
				{1},
			},
		},
		{
			name: "multiple txs",
			txs: []*txs.Tx{
				newTx(ids.ID{1}),
				newTx(ids.ID{2}),
				newTx(ids.ID{3}),
			},
			wantTxs: []ids.ID{
				{3},
				{2},
				{1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			m, err := New(
				"",
				gas.Dimensions{1, 1, 1, 1},
				1_000_000,
				snowtest.AVAXAssetID,
				prometheus.NewRegistry(),
			)
			require.NoError(err)

			for _, tx := range tt.txs {
				require.NoError(m.Add(tx))
			}

			var gotTxs []ids.ID
			m.Iterate(func(tx *txs.Tx) bool {
				gotTxs = append(gotTxs, tx.ID())
				return true
			})

			require.Equal(tt.wantTxs, gotTxs)
		})
	}
}

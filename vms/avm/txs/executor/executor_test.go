// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm/blocks"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/states"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	chainID = ids.ID{5, 4, 3, 2, 1}
	assetID = ids.ID{1, 2, 3}
)

func TestBaseTxExecutor(t *testing.T) {
	secpFx := &secp256k1fx.Fx{}
	parser, err := blocks.NewParser([]fxs.Fx{secpFx})
	require.NoError(t, err)
	codec := parser.Codec()

	db := memdb.New()
	vdb := versiondb.New(db)
	registerer := prometheus.NewRegistry()
	state, err := states.New(vdb, parser, registerer)
	require.NoError(t, err)

	utxoID := avax.UTXOID{
		TxID:        ids.Empty,
		OutputIndex: 1,
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: ids.Empty},
		Out:    &avax.TestVerifiable{},
	}

	baseTx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    constants.UnitTestID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: utxoID,
			Asset:  avax.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt: 20 * units.KiloAvax,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}}
	if err := baseTx.SignSECP256K1Fx(parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}); err != nil {
		t.Fatal(err)
	}

	txID := baseTx.ID()

	state.AddUTXO(utxo)
	state.AddTx(baseTx)
	state.AddStatus(txID, choices.Accepted)

	executor := &Executor{
		Codec: codec,
		State: state,
		Tx:    baseTx,
	}

	// Execute baseTx
	err = baseTx.Unsigned.Visit(executor)
	require.NoError(t, err)
	_, err = executor.State.GetUTXO(utxo.InputID())
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestCreateAssetTxExecutor(t *testing.T) {
	secpFx := &secp256k1fx.Fx{}
	parser, err := blocks.NewParser([]fxs.Fx{secpFx})
	require.NoError(t, err)
	codec := parser.Codec()

	db := memdb.New()
	vdb := versiondb.New(db)
	registerer := prometheus.NewRegistry()
	state, err := states.New(vdb, parser, registerer)
	require.NoError(t, err)

	utxoID := avax.UTXOID{
		TxID:        ids.Empty,
		OutputIndex: 1,
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: ids.Empty},
		Out:    &avax.TestVerifiable{},
	}

	addr := keys[0].PublicKey().Address()

	unsignedTx := &txs.CreateAssetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: utxoID,
				Asset:  avax.Asset{ID: assetID},
				In: &secp256k1fx.TransferInput{
					Amt: 20 * units.KiloAvax,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{
							0,
						},
					},
				},
			}},
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 20 * units.KiloAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				},
			}},
		}},
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
							Addrs:     []ids.ShortID{addr},
						},
					},
				},
			},
		},
	}

	createAssetTx := &txs.Tx{Unsigned: unsignedTx}

	if err := createAssetTx.SignSECP256K1Fx(parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}); err != nil {
		t.Fatal(err)
	}

	txID := createAssetTx.ID()

	state.AddUTXO(utxo)
	state.AddTx(createAssetTx)
	state.AddStatus(txID, choices.Accepted)

	executor := &Executor{
		Codec: codec,
		State: state,
		Tx:    createAssetTx,
	}

	// Execute createAssetTx
	err = createAssetTx.Unsigned.Visit(executor)
	require.NoError(t, err)

	// Verify consumed UTXO is removed from state
	_, err = executor.State.GetUTXO(utxo.InputID())
	require.ErrorIs(t, err, database.ErrNotFound)

	// Verify produced UTXO is added to state
	outputUTXO := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: uint32(0),
		},
		Asset: avax.Asset{
			ID: txID,
		},
	}
	_, err = executor.State.GetUTXO(outputUTXO.InputID())
	require.NoError(t, err)
}

func TestOperationTxExecutor(t *testing.T) {
	secpFx := &secp256k1fx.Fx{}
	parser, err := blocks.NewParser([]fxs.Fx{secpFx})
	require.NoError(t, err)
	codec := parser.Codec()

	db := memdb.New()
	vdb := versiondb.New(db)
	registerer := prometheus.NewRegistry()
	state, err := states.New(vdb, parser, registerer)
	require.NoError(t, err)

	utxoID := avax.UTXOID{
		TxID:        ids.Empty,
		OutputIndex: 1,
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: assetID},
		Out:    &avax.TestVerifiable{},
	}

	addr := keys[0].PublicKey().Address()
	opUTXOID := avax.UTXOID{
		TxID:        ids.GenerateTestID(),
		OutputIndex: 0,
	}
	opUTXOID.OutputIndex++
	opUTXO := &avax.UTXO{
		UTXOID: opUTXOID,
		Asset:  avax.Asset{ID: assetID},
		Out:    &avax.TestVerifiable{},
	}
	inputSigners := secp256k1fx.Input{
		SigIndices: []uint32{2},
	}
	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}
	fxOutput := secp256k1fx.TransferOutput{
		Amt:          12345,
		OutputOwners: outputOwners,
	}
	fxOp := secp256k1fx.MintOperation{
		MintInput: inputSigners,
		MintOutput: secp256k1fx.MintOutput{
			OutputOwners: outputOwners,
		},
		TransferOutput: fxOutput,
	}
	op := txs.Operation{
		Asset: avax.Asset{ID: assetID},
		UTXOIDs: []*avax.UTXOID{
			&opUTXOID,
		},
		Op: &fxOp,
	}

	unsignedTx := &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins: []*avax.TransferableInput{{
				UTXOID: utxoID,
				Asset:  avax.Asset{ID: assetID},
				In: &secp256k1fx.TransferInput{
					Amt: 20 * units.KiloAvax,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{
							0,
						},
					},
				},
			}},
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 20 * units.KiloAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				},
			}},
		}},
		Ops: []*txs.Operation{
			&op,
		},
	}

	operationTx := &txs.Tx{Unsigned: unsignedTx}

	if err := operationTx.SignSECP256K1Fx(parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}); err != nil {
		t.Fatal(err)
	}

	txID := operationTx.ID()

	state.AddUTXO(utxo)
	state.AddUTXO(opUTXO)
	state.AddTx(operationTx)
	state.AddStatus(txID, choices.Accepted)

	executor := &Executor{
		Codec: codec,
		State: state,
		Tx:    operationTx,
	}

	// Execute operationTx
	err = operationTx.Unsigned.Visit(executor)
	require.NoError(t, err)

	// Verify consumed UTXO is removed from state
	_, err = executor.State.GetUTXO(utxo.InputID())
	require.ErrorIs(t, err, database.ErrNotFound)
	_, err = executor.State.GetUTXO(opUTXO.InputID())
	require.ErrorIs(t, err, database.ErrNotFound)
}

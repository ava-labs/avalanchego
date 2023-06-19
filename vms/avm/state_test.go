// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestSetsAndGets(t *testing.T) {
	require := require.New(t)

	_, _, vm, _ := GenesisVMWithArgs(
		t,
		[]*common.Fx{{
			ID: ids.GenerateTestID(),
			Fx: &FxTest{
				InitializeF: func(vmIntf interface{}) error {
					vm := vmIntf.(secp256k1fx.VM)
					return vm.CodecRegistry().RegisterType(&avax.TestState{})
				},
			},
		}},
		nil,
	)
	ctx := vm.ctx
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	state := vm.state

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: avax.Asset{ID: ids.Empty},
		Out:   &avax.TestState{},
	}
	utxoID := utxo.InputID()

	tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    constants.UnitTestID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: assetID},
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
	require.NoError(tx.SignSECP256K1Fx(vm.parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}))

	txID := tx.ID()

	state.AddUTXO(utxo)
	state.AddTx(tx)
	state.AddStatus(txID, choices.Accepted)

	resultUTXO, err := state.GetUTXO(utxoID)
	require.NoError(err)
	resultTx, err := state.GetTx(txID)
	require.NoError(err)
	resultStatus, err := state.GetStatus(txID)
	require.NoError(err)

	require.Equal(uint32(1), resultUTXO.OutputIndex)
	require.Equal(tx.ID(), resultTx.ID())
	require.Equal(choices.Accepted, resultStatus)
}

func TestFundingNoAddresses(t *testing.T) {
	_, _, vm, _ := GenesisVMWithArgs(
		t,
		[]*common.Fx{{
			ID: ids.GenerateTestID(),
			Fx: &FxTest{
				InitializeF: func(vmIntf interface{}) error {
					vm := vmIntf.(secp256k1fx.VM)
					return vm.CodecRegistry().RegisterType(&avax.TestState{})
				},
			},
		}},
		nil,
	)
	ctx := vm.ctx
	defer func() {
		require.NoError(t, vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	state := vm.state

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: avax.Asset{ID: ids.Empty},
		Out:   &avax.TestState{},
	}

	state.AddUTXO(utxo)
	state.DeleteUTXO(utxo.InputID())
}

func TestFundingAddresses(t *testing.T) {
	require := require.New(t)

	_, _, vm, _ := GenesisVMWithArgs(
		t,
		[]*common.Fx{{
			ID: ids.GenerateTestID(),
			Fx: &FxTest{
				InitializeF: func(vmIntf interface{}) error {
					vm := vmIntf.(secp256k1fx.VM)
					return vm.CodecRegistry().RegisterType(&avax.TestAddressable{})
				},
			},
		}},
		nil,
	)
	ctx := vm.ctx
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	state := vm.state

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: avax.Asset{ID: ids.Empty},
		Out: &avax.TestAddressable{
			Addrs: [][]byte{{0}},
		},
	}

	state.AddUTXO(utxo)
	require.NoError(state.Commit())

	utxos, err := state.UTXOIDs([]byte{0}, ids.Empty, math.MaxInt32)
	require.NoError(err)
	require.Len(utxos, 1)
	require.Equal(utxo.InputID(), utxos[0])

	state.DeleteUTXO(utxo.InputID())
	require.NoError(state.Commit())

	utxos, err = state.UTXOIDs([]byte{0}, ids.Empty, math.MaxInt32)
	require.NoError(err)
	require.Empty(utxos)
}

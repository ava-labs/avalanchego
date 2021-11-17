// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestSetsAndGets(t *testing.T) {
	_, _, vm, _ := GenesisVMWithArgs(
		t,
		[]*common.Fx{{
			ID: ids.GenerateTestID(),
			Fx: &FxTest{
				InitializeF: func(vmIntf interface{}) error {
					vm := vmIntf.(secp256k1fx.VM)
					return vm.CodecRegistry().RegisterType(&avax.TestVerifiable{})
				},
			},
		}},
		nil,
	)
	ctx := vm.ctx
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	state := vm.state

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: avax.Asset{ID: ids.Empty},
		Out:   &avax.TestVerifiable{},
	}

	tx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
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
	if err := tx.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{keys[0]}}); err != nil {
		t.Fatal(err)
	}

	if err := state.PutUTXO(ids.Empty, utxo); err != nil {
		t.Fatal(err)
	}
	if err := state.PutTx(ids.Empty, tx); err != nil {
		t.Fatal(err)
	}
	if err := state.PutStatus(ids.Empty, choices.Accepted); err != nil {
		t.Fatal(err)
	}

	resultUTXO, err := state.GetUTXO(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}
	resultTx, err := state.GetTx(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}
	resultStatus, err := state.GetStatus(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if resultUTXO.OutputIndex != 1 {
		t.Fatalf("Wrong UTXO returned")
	}
	if resultTx.ID() != tx.ID() {
		t.Fatalf("Wrong Tx returned")
	}
	if resultStatus != choices.Accepted {
		t.Fatalf("Wrong Status returned")
	}
}

func TestFundingNoAddresses(t *testing.T) {
	_, _, vm, _ := GenesisVMWithArgs(
		t,
		[]*common.Fx{{
			ID: ids.GenerateTestID(),
			Fx: &FxTest{
				InitializeF: func(vmIntf interface{}) error {
					vm := vmIntf.(secp256k1fx.VM)
					return vm.CodecRegistry().RegisterType(&avax.TestVerifiable{})
				},
			},
		}},
		nil,
	)
	ctx := vm.ctx
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	state := vm.state

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: avax.Asset{ID: ids.Empty},
		Out:   &avax.TestVerifiable{},
	}

	if err := state.PutUTXO(utxo.InputID(), utxo); err != nil {
		t.Fatal(err)
	}
	if err := state.DeleteUTXO(utxo.InputID()); err != nil {
		t.Fatal(err)
	}
}

func TestFundingAddresses(t *testing.T) {
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
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
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

	if err := state.PutUTXO(utxo.InputID(), utxo); err != nil {
		t.Fatal(err)
	}
	utxos, err := state.UTXOIDs([]byte{0}, ids.Empty, math.MaxInt32)
	if err != nil {
		t.Fatal(err)
	}
	if len(utxos) != 1 {
		t.Fatalf("Should have returned 1 utxoIDs")
	}
	if utxoID := utxos[0]; utxoID != utxo.InputID() {
		t.Fatalf("Returned wrong utxoID")
	}
	if err := state.DeleteUTXO(utxo.InputID()); err != nil {
		t.Fatal(err)
	}
	utxos, err = state.UTXOIDs([]byte{0}, ids.Empty, math.MaxInt32)
	if err != nil {
		t.Fatal(err)
	}
	if len(utxos) != 0 {
		t.Fatalf("Should have returned 0 utxoIDs")
	}
}

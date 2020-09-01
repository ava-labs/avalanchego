// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"math"
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestPrefixedSetsAndGets(t *testing.T) {
	_, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	state := vm.state

	vm.codec.RegisterType(&avax.TestVerifiable{})

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
			Asset: avax.Asset{ID: asset},
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

	if err := state.SetUTXO(ids.Empty, utxo); err != nil {
		t.Fatal(err)
	}
	if err := state.SetTx(ids.Empty, tx); err != nil {
		t.Fatal(err)
	}
	if err := state.SetStatus(ids.Empty, choices.Accepted); err != nil {
		t.Fatal(err)
	}

	resultUTXO, err := state.UTXO(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}
	resultTx, err := state.Tx(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}
	resultStatus, err := state.Status(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if resultUTXO.OutputIndex != 1 {
		t.Fatalf("Wrong UTXO returned")
	}
	if !resultTx.ID().Equals(tx.ID()) {
		t.Fatalf("Wrong Tx returned")
	}
	if resultStatus != choices.Accepted {
		t.Fatalf("Wrong Status returned")
	}
}

func TestPrefixedFundingNoAddresses(t *testing.T) {
	_, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	state := vm.state

	vm.codec.RegisterType(&avax.TestVerifiable{})

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: avax.Asset{ID: ids.Empty},
		Out:   &avax.TestVerifiable{},
	}

	if err := state.FundUTXO(utxo); err != nil {
		t.Fatal(err)
	}
	if err := state.SpendUTXO(utxo.InputID()); err != nil {
		t.Fatal(err)
	}
}

func TestPrefixedFundingAddresses(t *testing.T) {
	_, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	state := vm.state

	vm.codec.RegisterType(&avax.TestAddressable{})

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

	if err := state.FundUTXO(utxo); err != nil {
		t.Fatal(err)
	}
	funds, err := state.Funds([]byte{0}, ids.Empty, math.MaxInt32)
	if err != nil {
		t.Fatal(err)
	}
	if len(funds) != 1 {
		t.Fatalf("Should have returned 1 utxoIDs")
	}
	if utxoID := funds[0]; !utxoID.Equals(utxo.InputID()) {
		t.Fatalf("Returned wrong utxoID")
	}
	if err := state.SpendUTXO(utxo.InputID()); err != nil {
		t.Fatal(err)
	}
	funds, err = state.Funds([]byte{0}, ids.Empty, math.MaxInt32)
	if err != nil {
		t.Fatal(err)
	}
	if len(funds) != 0 {
		t.Fatalf("Should have returned 0 utxoIDs")
	}
}

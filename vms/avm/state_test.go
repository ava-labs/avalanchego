// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestStateIDs(t *testing.T) {
	_, _, vm := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	state := vm.state.state

	id0 := ids.NewID([32]byte{0x00, 0})
	id1 := ids.NewID([32]byte{0x01, 0})
	id2 := ids.NewID([32]byte{0x02, 0})

	if _, err := state.IDs(ids.Empty); err != nil {
		t.Fatal(err)
	}

	expected := []ids.ID{id0, id1}
	for _, id := range expected {
		if err := state.AddID(ids.Empty, id); err != nil {
			t.Fatal(err)
		}
	}

	result, err := state.IDs(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != len(expected) {
		t.Fatalf("Returned the wrong number of ids")
	}

	ids.SortIDs(result)
	for i, resultID := range result {
		expectedID := expected[i]
		if !expectedID.Equals(resultID) {
			t.Fatalf("Wrong ID returned")
		}
	}

	for _, id := range expected {
		if err := state.RemoveID(ids.Empty, id); err != nil {
			t.Fatal(err)
		}
	}

	result, err = state.IDs(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 0 {
		t.Fatalf("Should have returned 0 IDs")
	}

	expected = []ids.ID{id1, id2}
	for _, id := range expected {
		if err := state.AddID(ids.Empty, id); err != nil {
			t.Fatal(err)
		}
	}

	result, err = state.IDs(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != len(expected) {
		t.Fatalf("Returned the wrong number of ids")
	}

	ids.SortIDs(result)
	for i, resultID := range result {
		expectedID := expected[i]
		if !expectedID.Equals(resultID) {
			t.Fatalf("Wrong ID returned")
		}
	}

	state.Cache.Flush()

	result, err = state.IDs(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != len(expected) {
		t.Fatalf("Returned the wrong number of ids")
	}

	ids.SortIDs(result)
	for i, resultID := range result {
		expectedID := expected[i]
		if !expectedID.Equals(resultID) {
			t.Fatalf("Wrong ID returned")
		}
	}

	if err := state.SetStatus(ids.Empty, choices.Accepted); err != nil {
		t.Fatal(err)
	}

	statusResult, err := state.Status(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}
	if statusResult != choices.Accepted {
		t.Fatalf("Should have returned the %s status", choices.Accepted)
	}

	for _, id := range expected {
		if err := state.RemoveID(ids.Empty, id); err != nil {
			t.Fatal(err)
		}
	}

	result, err = state.IDs(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 0 {
		t.Fatalf("Should have returned 0 IDs")
	}

	if err := state.AddID(ids.Empty, ids.ID{}); err == nil {
		t.Fatalf("Should have errored during serialization")
	}

	if err := state.RemoveID(ids.Empty, ids.ID{}); err == nil {
		t.Fatalf("Should have errored during serialization")
	}
}

func TestStateStatuses(t *testing.T) {
	_, _, vm := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	state := vm.state.state

	if _, err := state.Status(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading ids")
	}

	if err := state.SetStatus(ids.Empty, choices.Accepted); err != nil {
		t.Fatal(err)
	}

	status, err := state.Status(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}
	if status != choices.Accepted {
		t.Fatalf("Should have returned the %s status", choices.Accepted)
	}

	if err := state.AddID(ids.Empty, ids.Empty); err != nil {
		t.Fatal(err)
	}

	status, err = state.Status(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}
	if status != choices.Accepted {
		t.Fatalf("Should have returned the %s status", choices.Accepted)
	}

	if err := state.SetStatus(ids.Empty, choices.Unknown); err != nil {
		t.Fatal(err)
	}

	if _, err := state.Status(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading ids")
	}
}

func TestStateUTXOs(t *testing.T) {
	_, _, vm := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	state := vm.state.state

	vm.codec.RegisterType(&ava.TestVerifiable{})

	if _, err := state.UTXO(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading utxo")
	}

	utxo := &ava.UTXO{
		UTXOID: ava.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: ava.Asset{ID: ids.Empty},
		Out:   &ava.TestVerifiable{},
	}

	if err := state.SetUTXO(ids.Empty, utxo); err != nil {
		t.Fatal(err)
	}

	result, err := state.UTXO(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if result.OutputIndex != 1 {
		t.Fatalf("Wrong UTXO returned")
	}

	state.Cache.Flush()

	result, err = state.UTXO(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if result.OutputIndex != 1 {
		t.Fatalf("Wrong UTXO returned")
	}

	if err := state.SetUTXO(ids.Empty, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := state.UTXO(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading utxo")
	}

	if err := state.SetUTXO(ids.Empty, &ava.UTXO{}); err == nil {
		t.Fatalf("Should have errored packing the utxo")
	}

	if err := state.SetStatus(ids.Empty, choices.Accepted); err != nil {
		t.Fatal(err)
	}

	if _, err := state.UTXO(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading utxo")
	}

	state.Cache.Flush()

	if _, err := state.UTXO(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading utxo")
	}
}

func TestStateTXs(t *testing.T) {
	_, _, vm := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	state := vm.state.state

	vm.codec.RegisterType(&ava.TestTransferable{})

	if _, err := state.Tx(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading tx")
	}

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 0,
			},
			Asset: ava.Asset{ID: asset},
			In: &secp256k1fx.TransferInput{
				Amt: 20 * units.KiloAva,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(tx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	tx.Creds = append(tx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	if err := state.SetTx(ids.Empty, tx); err != nil {
		t.Fatal(err)
	}

	result, err := state.Tx(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if !result.ID().Equals(tx.ID()) {
		t.Fatalf("Wrong Tx returned")
	}

	state.Cache.Flush()

	result, err = state.Tx(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if !result.ID().Equals(tx.ID()) {
		t.Fatalf("Wrong Tx returned")
	}

	if err := state.SetTx(ids.Empty, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := state.Tx(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading tx")
	}

	if err := state.SetStatus(ids.Empty, choices.Accepted); err != nil {
		t.Fatal(err)
	}

	if _, err := state.Tx(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading tx")
	}

	state.Cache.Flush()

	if _, err := state.Tx(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading tx")
	}
}

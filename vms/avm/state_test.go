// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestStateIDs(t *testing.T) {
	vm := GenesisVM(t)
	state := vm.state.state

	id0 := ids.NewID([32]byte{0xff, 0})
	id1 := ids.NewID([32]byte{0xff, 0})
	id2 := ids.NewID([32]byte{0xff, 0})

	if _, err := state.IDs(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading ids")
	}

	expected := []ids.ID{id0, id1}
	if err := state.SetIDs(ids.Empty, expected); err != nil {
		t.Fatal(err)
	}

	result, err := state.IDs(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != len(expected) {
		t.Fatalf("Returned the wrong number of ids")
	}

	for i, resultID := range result {
		expectedID := expected[i]
		if !expectedID.Equals(resultID) {
			t.Fatalf("Wrong ID returned")
		}
	}

	expected = []ids.ID{id1, id2}
	if err := state.SetIDs(ids.Empty, expected); err != nil {
		t.Fatal(err)
	}

	result, err = state.IDs(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != len(expected) {
		t.Fatalf("Returned the wrong number of ids")
	}

	for i, resultID := range result {
		expectedID := expected[i]
		if !expectedID.Equals(resultID) {
			t.Fatalf("Wrong ID returned")
		}
	}

	state.c.Flush()

	result, err = state.IDs(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != len(expected) {
		t.Fatalf("Returned the wrong number of ids")
	}

	for i, resultID := range result {
		expectedID := expected[i]
		if !expectedID.Equals(resultID) {
			t.Fatalf("Wrong ID returned")
		}
	}

	if err := state.SetStatus(ids.Empty, choices.Accepted); err != nil {
		t.Fatal(err)
	}

	result, err = state.IDs(ids.Empty)
	if err == nil {
		t.Fatalf("Should have errored during cache lookup")
	}

	state.c.Flush()

	result, err = state.IDs(ids.Empty)
	if err == nil {
		t.Fatalf("Should have errored during parsing")
	}

	statusResult, err := state.Status(ids.Empty)
	if err != nil {
		t.Fatal(err)
	}
	if statusResult != choices.Accepted {
		t.Fatalf("Should have returned the %s status", choices.Accepted)
	}

	if err := state.SetIDs(ids.Empty, []ids.ID{ids.ID{}}); err == nil {
		t.Fatalf("Should have errored during serialization")
	}

	if err := state.SetIDs(ids.Empty, []ids.ID{}); err != nil {
		t.Fatal(err)
	}

	if _, err := state.IDs(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading ids")
	}
}

func TestStateStatuses(t *testing.T) {
	vm := GenesisVM(t)
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

	if err := state.SetIDs(ids.Empty, []ids.ID{ids.Empty}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Status(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading ids")
	}

	if err := state.SetStatus(ids.Empty, choices.Accepted); err != nil {
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
	vm := GenesisVM(t)
	state := vm.state.state

	vm.codec.RegisterType(&testVerifiable{})

	if _, err := state.UTXO(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading utxo")
	}

	utxo := &UTXO{
		UTXOID: UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: Asset{ID: ids.Empty},
		Out:   &testVerifiable{},
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

	state.c.Flush()

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

	if err := state.SetUTXO(ids.Empty, &UTXO{}); err == nil {
		t.Fatalf("Should have errored packing the utxo")
	}

	if err := state.SetStatus(ids.Empty, choices.Accepted); err != nil {
		t.Fatal(err)
	}

	if _, err := state.UTXO(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading utxo")
	}

	state.c.Flush()

	if _, err := state.UTXO(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading utxo")
	}
}

func TestStateTXs(t *testing.T) {
	vm := GenesisVM(t)
	state := vm.state.state

	vm.codec.RegisterType(&TestTransferable{})

	if _, err := state.Tx(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading tx")
	}

	tx := &Tx{UnsignedTx: &OperationTx{BaseTx: BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*TransferableInput{
			&TransferableInput{
				UTXOID: UTXOID{
					TxID:        ids.Empty,
					OutputIndex: 0,
				},
				Asset: Asset{
					ID: asset,
				},
				In: &secp256k1fx.TransferInput{
					Amt: 20 * units.KiloAva,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{
							0,
						},
					},
				},
			},
		},
	}}}

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

	state.c.Flush()

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

	state.c.Flush()

	if _, err := state.Tx(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading tx")
	}
}

// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestPrefixedSetsAndGets(t *testing.T) {
	_, _, vm := GenesisVM(t)
	ctx.Lock.Unlock()
	defer func() { ctx.Lock.Lock(); vm.Shutdown(); ctx.Lock.Unlock() }()

	// FIXME? is it safe to access vm.state.state without the lock?
	state := vm.state

	// FIXME? is it safe to call vm.codec.RegisterType() without the lock?
	vm.codec.RegisterType(&ava.TestVerifiable{})

	utxo := &ava.UTXO{
		UTXOID: ava.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: ava.Asset{ID: ids.Empty},
		Out:   &ava.TestVerifiable{},
	}

	tx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{&ava.TransferableInput{
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

	// FIXME? Is it safe to call vm.codec.Marshal() without the lock?
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

	// FIXME? Is it safe to call vm.codec.Marshal() without the lock?
	b, err := vm.codec.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

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
	_, _, vm := GenesisVM(t)
	ctx.Lock.Unlock()
	defer func() { ctx.Lock.Lock(); vm.Shutdown(); ctx.Lock.Unlock() }()

	// FIXME? is it safe to access vm.state.state without the lock?
	state := vm.state

	// FIXME? is it safe to call vm.codec.RegisterType() without the lock?
	vm.codec.RegisterType(&ava.TestVerifiable{})

	utxo := &ava.UTXO{
		UTXOID: ava.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: ava.Asset{ID: ids.Empty},
		Out:   &ava.TestVerifiable{},
	}

	if err := state.FundUTXO(utxo); err != nil {
		t.Fatal(err)
	}
	if err := state.SpendUTXO(utxo.InputID()); err != nil {
		t.Fatal(err)
	}
}

func TestPrefixedFundingAddresses(t *testing.T) {
	_, _, vm := GenesisVM(t)
	ctx.Lock.Unlock()
	defer func() { ctx.Lock.Lock(); vm.Shutdown(); ctx.Lock.Unlock() }()

	// FIXME? is it safe to access vm.state.state without the lock?
	state := vm.state

	// FIXME? is it safe to call vm.codec.RegisterType() without the lock?
	vm.codec.RegisterType(&testAddressable{})

	utxo := &ava.UTXO{
		UTXOID: ava.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: ava.Asset{ID: ids.Empty},
		Out: &testAddressable{
			Addrs: [][]byte{
				[]byte{0},
			},
		},
	}

	if err := state.FundUTXO(utxo); err != nil {
		t.Fatal(err)
	}
	funds, err := state.Funds(ids.NewID(hashing.ComputeHash256Array([]byte{0})))
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
	_, err = state.Funds(ids.NewID(hashing.ComputeHash256Array([]byte{0})))
	if err == nil {
		t.Fatalf("Should have returned no utxoIDs")
	}
}

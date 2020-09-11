// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Test function IDs when argument start is empty
func TestStateIDsNoStart(t *testing.T) {
	_, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	state := vm.state.state

	id0 := ids.NewID([32]byte{0x00, 0})
	id1 := ids.NewID([32]byte{0x01, 0})
	id2 := ids.NewID([32]byte{0x02, 0})

	if _, err := state.IDs(ids.Empty.Bytes(), []byte{}, math.MaxInt32); err != nil {
		t.Fatal(err)
	}

	expected := []ids.ID{id0, id1}
	for _, id := range expected {
		if err := state.AddID(ids.Empty.Bytes(), id); err != nil {
			t.Fatal(err)
		}
	}

	result, err := state.IDs(ids.Empty.Bytes(), []byte{}, 0)
	if err != nil {
		t.Fatal(err)
	} else if len(result) != 0 {
		t.Fatal("result should have length 0 because limit is 0")
	}

	result, err = state.IDs(ids.Empty.Bytes(), []byte{}, 1)
	if err != nil {
		t.Fatal(err)
	} else if len(result) != 1 {
		t.Fatal("result should have length 0 because limit is 1")
	}

	result, err = state.IDs(ids.Empty.Bytes(), []byte{}, math.MaxInt32)
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
		if err := state.RemoveID(ids.Empty.Bytes(), id); err != nil {
			t.Fatal(err)
		}
	}

	result, err = state.IDs(ids.Empty.Bytes(), []byte{}, math.MaxInt32)
	if err != nil {
		t.Fatal(err)
	} else if len(result) != 0 {
		t.Fatalf("Should have returned 0 IDs")
	}

	expected = []ids.ID{id1, id2}
	for _, id := range expected {
		if err := state.AddID(ids.Empty.Bytes(), id); err != nil {
			t.Fatal(err)
		}
	}

	result, err = state.IDs(ids.Empty.Bytes(), []byte{}, math.MaxInt32)
	if err != nil {
		t.Fatal(err)
	} else if len(result) != len(expected) {
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

	result, err = state.IDs(ids.Empty.Bytes(), []byte{}, math.MaxInt32)
	if err != nil {
		t.Fatal(err)
	} else if len(result) != len(expected) {
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
	} else if statusResult != choices.Accepted {
		t.Fatalf("Should have returned the %s status", choices.Accepted)
	}

	for _, id := range expected {
		if err := state.RemoveID(ids.Empty.Bytes(), id); err != nil {
			t.Fatal(err)
		}
	}

	result, err = state.IDs(ids.Empty.Bytes(), []byte{}, math.MaxInt32)
	if err != nil {
		t.Fatal(err)
	} else if len(result) != 0 {
		t.Fatalf("Should have returned 0 IDs")
	} else if err := state.AddID(ids.Empty.Bytes(), ids.ID{}); err == nil {
		t.Fatalf("Should have errored during serialization")
	} else if err := state.RemoveID(ids.Empty.Bytes(), ids.ID{}); err == nil {
		t.Fatalf("Should have errored during serialization")
	}
}

func TestStateIDsWithStart(t *testing.T) {
	_, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	state := vm.state.state
	id0 := ids.NewID([32]byte{0x00, 0})
	id1 := ids.NewID([32]byte{0x01, 0})
	id2 := ids.NewID([32]byte{0x02, 0})

	// State should be empty to start
	if _, err := state.IDs(ids.Empty.Bytes(), []byte{}, math.MaxInt32); err != nil {
		t.Fatal(err)
	}

	// Put all three IDs
	if err := state.AddID(ids.Empty.Bytes(), id0); err != nil {
		t.Fatal(err)
	} else if err := state.AddID(ids.Empty.Bytes(), id1); err != nil {
		t.Fatal(err)
	} else if err := state.AddID(ids.Empty.Bytes(), id2); err != nil {
		t.Fatal(err)
	}

	if result, err := state.IDs(ids.Empty.Bytes(), []byte{}, math.MaxInt32); err != nil { // start at beginning
		t.Fatal(err)
	} else if len(result) != 3 {
		t.Fatalf("result should have all 3 IDs but has %d", len(result))
	} else if result, err := state.IDs(ids.Empty.Bytes(), id0.Bytes(), math.MaxInt32); err != nil { // start after id0
		t.Fatal(err)
	} else if len(result) != 2 {
		t.Fatalf("result should have 2 IDs but has %d", len(result))
	} else if (!result[0].Equals(id1) && !result[1].Equals(id1)) || (!result[0].Equals(id2) && !result[1].Equals(id2)) {
		t.Fatal("result should have id1 and id2")
	} else if result, err := state.IDs(ids.Empty.Bytes(), id1.Bytes(), math.MaxInt32); err != nil { // start after id1
		t.Fatal(err)
	} else if len(result) != 1 {
		t.Fatalf("result should have 1 IDs but has %d", len(result))
	} else if !result[0].Equals(id2) {
		t.Fatal("result should be id2")
	}
}

func TestStateStatuses(t *testing.T) {
	_, _, vm, _ := GenesisVM(t)
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

	if err := state.AddID(ids.Empty.Bytes(), ids.Empty); err != nil {
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
	_, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	state := vm.state.state

	vm.codec.RegisterType(&avax.TestVerifiable{})

	if _, err := state.UTXO(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading utxo")
	}

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: avax.Asset{ID: ids.Empty},
		Out:   &avax.TestVerifiable{},
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

	if err := state.SetUTXO(ids.Empty, &avax.UTXO{}); err == nil {
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
	_, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	state := vm.state.state

	vm.codec.RegisterType(&avax.TestTransferable{})

	if _, err := state.Tx(ids.Empty); err == nil {
		t.Fatalf("Should have errored when reading tx")
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

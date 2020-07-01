// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

/*
import (
	"testing"

	"github.com/ava-labs/gecko/ids"
)

func TestTxHeapStart(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	txHeap := EventHeap{SortByStartTime: true}

	validator0, err := vm.newAddDefaultSubnetValidatorTx(
		5,   // nonce
		123, // stake amount
		1,   // startTime
		3,   // endTime
		ids.NewShortID([20]byte{}),                    // node ID
		ids.NewShortID([20]byte{1, 2, 3, 4, 5, 6, 7}), // destination
		0,       // shares
		0,       // network ID
		keys[0], // key
	)
	if err != nil {
		t.Fatal(err)
	}

	validator1, err := vm.newAddDefaultSubnetValidatorTx(
		5,   // nonce
		123, // stake amount
		1,   // startTime
		3,   // endTime
		ids.NewShortID([20]byte{1}),                   // node ID
		ids.NewShortID([20]byte{1, 2, 3, 4, 5, 6, 7}), // destination
		0,       // shares
		0,       // network ID
		keys[0], // key
	)
	if err != nil {
		t.Fatal(err)
	}

	validator2, err := vm.newAddDefaultSubnetValidatorTx(
		5,   // nonce
		123, // stake amount
		2,   // startTime
		4,   // endTime
		ids.NewShortID([20]byte{}),                    // node ID
		ids.NewShortID([20]byte{1, 2, 3, 4, 5, 6, 7}), // destination
		0,       // shares
		0,       // network ID
		keys[0], // key
	)
	if err != nil {
		t.Fatal(err)
	}

	txHeap.Add(validator2)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(validator2.StartTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, validator2.StartTime())
	}

	txHeap.Add(validator1)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(validator1.StartTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, validator1.StartTime())
	}

	txHeap.Add(validator0)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(validator0.StartTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, validator0.StartTime())
	} else if top := txHeap.Peek(); !top.ID().Equals(validator0.ID()) {
		t.Fatalf("TxHeap prioritized %s, expected %s", top.ID(), validator0.ID())
	}
}

func TestTxHeapStop(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	txHeap := EventHeap{}

	validator0, err := vm.newAddDefaultSubnetValidatorTx(
		5,   // nonce
		123, // stake amount
		1,   // startTime
		3,   // endTime
		ids.NewShortID([20]byte{}),                    // node ID
		ids.NewShortID([20]byte{1, 2, 3, 4, 5, 6, 7}), // destination
		0,       // shares
		0,       // network ID
		keys[0], // key
	)
	if err != nil {
		t.Fatal(err)
	}

	validator1, err := vm.newAddDefaultSubnetValidatorTx(
		5,   // nonce
		123, // stake amount
		1,   // startTime
		3,   // endTime
		ids.NewShortID([20]byte{1}),                   // node ID
		ids.NewShortID([20]byte{1, 2, 3, 4, 5, 6, 7}), // destination
		0,       // shares
		0,       // network ID
		keys[0], // key
	)
	if err != nil {
		t.Fatal(err)
	}

	validator2, err := vm.newAddDefaultSubnetValidatorTx(
		5,   // nonce
		123, // stake amount
		2,   // startTime
		4,   // endTime
		ids.NewShortID([20]byte{}),                    // node ID
		ids.NewShortID([20]byte{1, 2, 3, 4, 5, 6, 7}), // destination
		0,       // shares
		0,       // network ID
		keys[0], // key
	)
	if err != nil {
		t.Fatal(err)
	}

	txHeap.Add(validator2)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(validator2.EndTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, validator2.EndTime())
	}

	txHeap.Add(validator1)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(validator1.EndTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, validator1.EndTime())
	}

	txHeap.Add(validator0)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(validator0.EndTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, validator0.EndTime())
	} else if top := txHeap.Txs[0]; !top.ID().Equals(validator0.ID()) {
		t.Fatalf("TxHeap prioritized %s, expected %s", top.ID(), validator0.ID())
	}
}

func TestTxHeapStartValidatorVsDelegatorOrdering(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	txHeap := EventHeap{SortByStartTime: true}

	validator, err := vm.newAddDefaultSubnetValidatorTx(
		5,   // nonce
		123, // stake amount
		1,   // startTime
		3,   // endTime
		ids.NewShortID([20]byte{}),                    // node ID
		ids.NewShortID([20]byte{1, 2, 3, 4, 5, 6, 7}), // destination
		0,       // shares
		0,       // network ID
		keys[0], // key
	)
	if err != nil {
		t.Fatal(err)
	}

	delegator, err := vm.newAddDefaultSubnetDelegatorTx(
		5,   // nonce
		123, // stake amount
		1,   // startTime
		3,   // endTime
		ids.NewShortID([20]byte{}),                    // node ID
		ids.NewShortID([20]byte{1, 2, 3, 4, 5, 6, 7}), // destination
		0,       // network ID
		keys[0], // key
	)
	if err != nil {
		t.Fatal(err)
	}

	txHeap.Add(validator)
	txHeap.Add(delegator)

	if top := txHeap.Txs[0]; !top.ID().Equals(validator.ID()) {
		t.Fatalf("TxHeap prioritized %s, expected %s", top.ID(), validator.ID())
	}
}

func TestTxHeapStopValidatorVsDelegatorOrdering(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	txHeap := EventHeap{}

	validator, err := vm.newAddDefaultSubnetValidatorTx(
		5,   // nonce
		123, // stake amount
		1,   // startTime
		3,   // endTime
		ids.NewShortID([20]byte{}),                    // node ID
		ids.NewShortID([20]byte{1, 2, 3, 4, 5, 6, 7}), // destination
		0,       // shares
		0,       // network ID
		keys[0], // key
	)
	if err != nil {
		t.Fatal(err)
	}

	delegator, err := vm.newAddDefaultSubnetDelegatorTx(
		5,   // nonce
		123, // stake amount
		1,   // startTime
		3,   // endTime
		ids.NewShortID([20]byte{}),                    // node ID
		ids.NewShortID([20]byte{1, 2, 3, 4, 5, 6, 7}), // destination
		0,       // network ID
		keys[0], // key
	)
	if err != nil {
		t.Fatal(err)
	}

	txHeap.Add(validator)
	txHeap.Add(delegator)

	if top := txHeap.Txs[0]; !top.ID().Equals(delegator.ID()) {
		t.Fatalf("TxHeap prioritized %s, expected %s", top.ID(), delegator.ID())
	}
}

// Ensure *AddValidatorTxHeap are marshaled/unmarshaled correctly
func TestMarshalAddValidatorTxHeap(t *testing.T) {
	validators := GenesisCurrentValidators()

	bytes, err := Codec.Marshal(validators)
	if err != nil {
		t.Fatal("err")
	}

	stakersUnmarshaled := EventHeap{}
	if err := Codec.Unmarshal(bytes, &stakersUnmarshaled); err != nil {
		t.Fatal(err)
	}

	for i, originalTx := range validators.Txs {
		unmarshaledTx := stakersUnmarshaled.Txs[i]
		unmarshaledTx.initialize(nil)
		if !originalTx.ID().Equals(unmarshaledTx.ID()) {
			t.Fatalf("Wrong IDs returned")
		}
	}
}
*/

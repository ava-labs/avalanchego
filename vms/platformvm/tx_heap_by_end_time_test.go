// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
)

func TestTxHeapStop(t *testing.T) {
	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	txHeap := newTxHeapByEndTime()

	validator0, err := vm.newAddValidatorTx(
		vm.MinValidatorStake,                                               // stake amount
		uint64(defaultGenesisTime.Unix()+1),                                // startTime
		uint64(defaultGenesisTime.Add(defaultMinStakingDuration).Unix()+1), // endTime
		ids.NodeID{},                            // node ID
		ids.ShortID{1, 2, 3, 4, 5, 6, 7},        // reward address
		0,                                       // shares
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // key
		ids.ShortEmpty,                          // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	statefulVdr0Tx, err := MakeStatefulTx(validator0)
	if err != nil {
		t.Fatal(err)
	}
	vdr0Tx := statefulVdr0Tx.(*StatefulAddValidatorTx)

	validator1, err := vm.newAddValidatorTx(
		vm.MinValidatorStake,                                               // stake amount
		uint64(defaultGenesisTime.Unix()+1),                                // startTime
		uint64(defaultGenesisTime.Add(defaultMinStakingDuration).Unix()+2), // endTime
		ids.NodeID{1},                           // node ID
		ids.ShortID{1, 2, 3, 4, 5, 6, 7},        // reward address
		0,                                       // shares
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // key
		ids.ShortEmpty,                          // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	statefulVdr1Tx, err := MakeStatefulTx(validator1)
	if err != nil {
		t.Fatal(err)
	}
	vdr1Tx := statefulVdr1Tx.(*StatefulAddValidatorTx)

	validator2, err := vm.newAddValidatorTx(
		vm.MinValidatorStake,                                               // stake amount
		uint64(defaultGenesisTime.Unix()+1),                                // startTime
		uint64(defaultGenesisTime.Add(defaultMinStakingDuration).Unix()+3), // endTime
		ids.NodeID{},                            // node ID
		ids.ShortID{1, 2, 3, 4, 5, 6, 7},        // reward address
		0,                                       // shares
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // key
		ids.ShortEmpty,                          // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	statefulVdr2Tx, err := MakeStatefulTx(validator2)
	if err != nil {
		t.Fatal(err)
	}
	vdr2Tx := statefulVdr2Tx.(*StatefulAddValidatorTx)

	txHeap.Add(validator2)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(vdr2Tx.EndTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, vdr2Tx.EndTime())
	}

	txHeap.Add(validator1)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(vdr1Tx.EndTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, vdr1Tx.EndTime())
	}

	txHeap.Add(validator0)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(vdr0Tx.EndTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, vdr0Tx.EndTime())
	} else if top := txHeap.txs[0].tx; top.Unsigned.ID() != validator0.Unsigned.ID() {
		t.Fatalf("TxHeap prioritized %s, expected %s", top.Unsigned.ID(), validator0.Unsigned.ID())
	}
}

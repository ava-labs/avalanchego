// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestTxHeapStop(t *testing.T) {
	h := newTestHelpersCollection()
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()

	heap := NewByEndTime()
	addValKeys := []*crypto.PrivateKeySECP256K1R{preFundedKeys[0]}

	validator0, err := h.txBuilder.NewAddValidatorTx(
		h.cfg.MinValidatorStake,                                            // stake amount
		uint64(defaultGenesisTime.Unix()+1),                                // startTime
		uint64(defaultGenesisTime.Add(defaultMinStakingDuration).Unix()+1), // endTime
		ids.NodeID{},                     // node ID
		ids.ShortID{1, 2, 3, 4, 5, 6, 7}, // reward address
		0,                                // shares
		addValKeys,
		ids.ShortEmpty,
	)
	if err != nil {
		t.Fatal(err)
	}
	vdr0Tx := validator0.Unsigned.(*txs.AddValidatorTx)

	validator1, err := h.txBuilder.NewAddValidatorTx(
		h.cfg.MinValidatorStake,                                            // stake amount
		uint64(defaultGenesisTime.Unix()+1),                                // startTime
		uint64(defaultGenesisTime.Add(defaultMinStakingDuration).Unix()+2), // endTime
		ids.NodeID{1},                    // node ID
		ids.ShortID{1, 2, 3, 4, 5, 6, 7}, // reward address
		0,                                // shares
		addValKeys,                       // Keys providing the staked tokens
		ids.ShortEmpty,
	)
	if err != nil {
		t.Fatal(err)
	}
	vdr1Tx := validator1.Unsigned.(*txs.AddValidatorTx)

	validator2, err := h.txBuilder.NewAddValidatorTx(
		h.cfg.MinValidatorStake,                                            // stake amount
		uint64(defaultGenesisTime.Unix()+1),                                // startTime
		uint64(defaultGenesisTime.Add(defaultMinStakingDuration).Unix()+3), // endTime
		ids.NodeID{},                     // node ID
		ids.ShortID{1, 2, 3, 4, 5, 6, 7}, // reward address
		0,                                // shares
		addValKeys,
		ids.ShortEmpty,
	)
	if err != nil {
		t.Fatal(err)
	}
	vdr2Tx := validator2.Unsigned.(*txs.AddValidatorTx)

	heap.Add(validator2)
	if timestamp := heap.Timestamp(); !timestamp.Equal(vdr2Tx.EndTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, vdr2Tx.EndTime())
	}

	heap.Add(validator1)
	if timestamp := heap.Timestamp(); !timestamp.Equal(vdr1Tx.EndTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, vdr1Tx.EndTime())
	}

	heap.Add(validator0)
	if timestamp := heap.Timestamp(); !timestamp.Equal(vdr0Tx.EndTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, vdr0Tx.EndTime())
	} else if top := heap.Peek(); top.ID() != validator0.ID() {
		t.Fatalf("TxHeap prioritized %s, expected %s", top.ID(), validator0.ID())
	}
}

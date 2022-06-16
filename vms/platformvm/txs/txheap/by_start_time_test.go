// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestByStartTime(t *testing.T) {
	assert := assert.New(t)

	txHeap := NewByStartTime()

	baseTime := time.Now()

	utx0 := &txs.AddValidatorTx{
		Validator: validator.Validator{
			NodeID: ids.NodeID{0},
			Start:  uint64(baseTime.Unix()) + 1,
			End:    uint64(baseTime.Unix()) + 1,
		},
		RewardsOwner: &secp256k1fx.OutputOwners{},
	}
	tx0 := &txs.Tx{Unsigned: utx0}
	err := tx0.Sign(txs.Codec, nil)
	assert.NoError(err)

	utx1 := &txs.AddValidatorTx{
		Validator: validator.Validator{
			NodeID: ids.NodeID{1},
			Start:  uint64(baseTime.Unix()) + 2,
			End:    uint64(baseTime.Unix()) + 2,
		},
		RewardsOwner: &secp256k1fx.OutputOwners{},
	}
	tx1 := &txs.Tx{Unsigned: utx1}
	err = tx1.Sign(txs.Codec, nil)
	assert.NoError(err)

	utx2 := &txs.AddValidatorTx{
		Validator: validator.Validator{
			NodeID: ids.NodeID{1},
			Start:  uint64(baseTime.Unix()) + 3,
			End:    uint64(baseTime.Unix()) + 3,
		},
		RewardsOwner: &secp256k1fx.OutputOwners{},
	}
	tx2 := &txs.Tx{Unsigned: utx2}
	err = tx2.Sign(txs.Codec, nil)
	assert.NoError(err)

	txHeap.Add(tx2)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(utx2.EndTime()) {
		t.Fatalf("txHeap.Timestamp returned %s, expected %s", timestamp, utx2.EndTime())
	}

	txHeap.Add(tx1)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(utx1.EndTime()) {
		t.Fatalf("txHeap.Timestamp returned %s, expected %s", timestamp, utx1.EndTime())
	}

	txHeap.Add(tx0)
	if timestamp := txHeap.Timestamp(); !timestamp.Equal(utx0.EndTime()) {
		t.Fatalf("TxHeap.Timestamp returned %s, expected %s", timestamp, utx0.EndTime())
	}
	if top := txHeap.Peek(); top.ID() != tx0.ID() {
		t.Fatalf("TxHeap prioritized %s, expected %s", top.ID(), tx0.ID())
	}
}

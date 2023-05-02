// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestByStartTime(t *testing.T) {
	require := require.New(t)

	txHeap := NewByStartTime()

	baseTime := time.Now()

	utx0 := &txs.AddValidatorTx{
		Validator: txs.Validator{
			NodeID: ids.NodeID{0},
			Start:  uint64(baseTime.Unix()) + 1,
			End:    uint64(baseTime.Unix()) + 1,
		},
		RewardsOwner: &secp256k1fx.OutputOwners{},
	}
	tx0 := &txs.Tx{Unsigned: utx0}
	err := tx0.Initialize(txs.Codec)
	require.NoError(err)

	utx1 := &txs.AddValidatorTx{
		Validator: txs.Validator{
			NodeID: ids.NodeID{1},
			Start:  uint64(baseTime.Unix()) + 2,
			End:    uint64(baseTime.Unix()) + 2,
		},
		RewardsOwner: &secp256k1fx.OutputOwners{},
	}
	tx1 := &txs.Tx{Unsigned: utx1}
	err = tx1.Initialize(txs.Codec)
	require.NoError(err)

	utx2 := &txs.AddValidatorTx{
		Validator: txs.Validator{
			NodeID: ids.NodeID{1},
			Start:  uint64(baseTime.Unix()) + 3,
			End:    uint64(baseTime.Unix()) + 3,
		},
		RewardsOwner: &secp256k1fx.OutputOwners{},
	}
	tx2 := &txs.Tx{Unsigned: utx2}
	err = tx2.Initialize(txs.Codec)
	require.NoError(err)

	txHeap.Add(tx2)
	require.Equal(utx2.EndTime(), txHeap.Timestamp())

	txHeap.Add(tx1)
	require.Equal(utx1.EndTime(), txHeap.Timestamp())

	txHeap.Add(tx0)
	require.Equal(utx0.EndTime(), txHeap.Timestamp())
	require.Equal(tx0, txHeap.Peek())
}

func TestByStartTimeList(t *testing.T) {
	require := require.New(t)

	txHeap := NewByStartTime()
	baseTime := time.Now()

	total := 100

	// map index to the cumulative size
	cumulativeSum := 0
	cumulativeSums := make([]int, total)

	for i := 0; i < total; i++ {
		utx := &txs.AddValidatorTx{
			Validator: txs.Validator{
				NodeID: ids.NodeID{0},
				Start:  uint64(baseTime.Unix()) + uint64(i) + 1,
				End:    uint64(baseTime.Unix()) + uint64(i) + 1,
			},
			RewardsOwner: &secp256k1fx.OutputOwners{},
		}
		tx := &txs.Tx{Unsigned: utx}
		require.NoError(tx.Initialize(txs.Codec))
		txHeap.Add(tx)

		cumulativeSum += tx.Size()
		cumulativeSums[i] = cumulativeSum
	}
	res, _ := txHeap.ListWithLimit(math.MaxInt32)
	require.Len(res, total)

	// only returns the txs up to the index and its sum
	maxSum := cumulativeSums[total-1]
	for idx, cumulativeSum := range cumulativeSums {
		// return all up to the index
		res, remaining := txHeap.ListWithLimit(cumulativeSum)
		require.Len(res, idx+1)
		require.Equal(0, remaining)

		// return all with maximum bytes
		res, remaining = txHeap.ListWithLimit(maxSum)
		require.Len(res, total)
		require.Equal(0, remaining)

		// return remaining with surplus bytes
		surplus := 77777
		res, remaining = txHeap.ListWithLimit(maxSum + surplus)
		require.Len(res, total)
		require.Equal(surplus, remaining)
	}
}

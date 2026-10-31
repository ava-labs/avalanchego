// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txgossip

import (
	"bytes"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// pendingTx describes a transaction in the output of [txpool.TxPool.Pending].
type pendingTx struct {
	tip  uint64
	seen time.Duration // after an arbitrary epoch
}

// pendingTxFeeCap is every [pendingTx]'s gas fee cap.
const pendingTxFeeCap = 100

func senderAddress(i int) common.Address {
	return common.BytesToAddress([]byte{byte(i + 1)})
}

// txHash identifies the transaction with the given nonce of the i-th sender.
func txHash(i, nonce int) common.Hash {
	return common.Hash{byte(i + 1), byte(nonce)}
}

// pendingSet builds the per-sender, nonce-ordered map returned by
// [txpool.TxPool.Pending], with hashes from [txHash].
func pendingSet(senders [][]pendingTx) map[common.Address][]*txpool.LazyTransaction {
	epoch := time.Unix(1_700_000_000, 0)
	pending := make(map[common.Address][]*txpool.LazyTransaction, len(senders))
	for i, txs := range senders {
		lazy := make([]*txpool.LazyTransaction, len(txs))
		for nonce, tx := range txs {
			lazy[nonce] = &txpool.LazyTransaction{
				Hash:      txHash(i, nonce),
				Time:      epoch.Add(tx.seen),
				GasFeeCap: uint256.NewInt(pendingTxFeeCap),
				GasTipCap: uint256.NewInt(tx.tip),
			}
		}
		pending[senderAddress(i)] = lazy
	}
	return pending
}

func hashes(tb testing.TB, ltxs []*LazyTransaction) []common.Hash {
	tb.Helper()
	out := make([]common.Hash, len(ltxs))
	for i, ltx := range ltxs {
		require.Equalf(tb, senderAddress(int(ltx.Hash[0])-1), ltx.Sender, "%T.Sender of %v", ltx, ltx.Hash)
		out[i] = ltx.Hash
	}
	return out
}

// referenceOrder restates, quadratically, the order documented by
// [Set.TransactionsByPriority].
func referenceOrder(pending map[common.Address][]*txpool.LazyTransaction, baseFee *uint256.Int) []common.Hash {
	taken := make(map[common.Address]int, len(pending))
	var order []common.Hash
	for {
		var (
			best    *LazyTransaction
			bestTip *uint256.Int
		)
		for from, txs := range pending {
			nonce := taken[from]
			if nonce == len(txs) {
				continue
			}
			ltx := &LazyTransaction{LazyTransaction: txs[nonce], Sender: from}
			tip := ltx.effectiveGasTip(baseFee)
			if best == nil || precedes(ltx, tip, best, bestTip) {
				best, bestTip = ltx, tip
			}
		}
		if best == nil {
			return order
		}
		order = append(order, best.Hash)
		taken[best.Sender]++
	}
}

func precedes(a *LazyTransaction, aTip *uint256.Int, b *LazyTransaction, bTip *uint256.Int) bool {
	if tip := aTip.Cmp(bTip); tip != 0 {
		return tip > 0
	}
	if !a.Time.Equal(b.Time) {
		return a.Time.Before(b.Time)
	}
	return bytes.Compare(a.Sender[:], b.Sender[:]) < 0
}

func TestTransactionsByPriorityEqualTipsFirstSeen(t *testing.T) {
	// Senders 1 and 2 bid the same tip, sender 2 first. Sender 0's tips differ
	// by nonce, which made a comparison sort's output depend on the order of
	// ranging over the map, so sender 1 could come out first.
	senders := [][]pendingTx{
		{{tip: 1, seen: 0}, {tip: 10, seen: 3}},
		{{tip: 5, seen: 2}},
		{{tip: 5, seen: 1}},
	}
	want := []common.Hash{txHash(2, 0), txHash(1, 0), txHash(0, 0), txHash(0, 1)}

	// Every map has its own iteration order, so build a new one each time.
	for range 100 {
		got := byPriority(pendingSet(senders), nil)
		require.Equal(t, want, hashes(t, got), "byPriority()")
	}
}

func TestTransactionsByPriorityNonceOrder(t *testing.T) {
	// With more than 20 transactions, [slices.SortStableFunc] merges sorted
	// blocks, and the old comparison let a sender's later nonce move ahead of
	// an earlier one in about half of map iteration orders of this pool.
	senders := [][]pendingTx{ // {tip, seen}
		{{2, 7}, {3, 2}, {1, 4}},
		{{1, 1}, {2, 4}},
		{{1, 3}, {3, 7}, {2, 0}},
		{{1, 6}},
		{{1, 3}},
		{{3, 8}, {3, 4}, {2, 6}},
		{{1, 6}, {3, 6}},
		{{2, 9}},
		{{2, 6}, {1, 6}, {2, 9}},
		{{3, 8}, {3, 8}},
	}

	for range 100 {
		pending := pendingSet(senders)
		got := hashes(t, byPriority(pending, nil))

		next := make(map[byte]byte)
		for _, h := range got {
			sender, nonce := h[0], h[1]
			require.Equalf(t, next[sender], nonce, "nonce of sender %d", sender-1)
			next[sender]++
		}
		require.Equal(t, referenceOrder(pending, nil), got, "byPriority()")
	}
}

func TestTransactionsByPriorityMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(0, 0)) //#nosec G404 -- Reproducibility is valuable for tests

	for i := range 2_000 {
		senders := make([][]pendingTx, 1+rng.IntN(8))
		for s := range senders {
			senders[s] = make([]pendingTx, 1+rng.IntN(4))
			for nonce := range senders[s] {
				senders[s][nonce] = pendingTx{
					tip:  1 + rng.Uint64N(3),
					seen: time.Duration(rng.IntN(16)),
				}
			}
		}
		// A base fee near the fee cap makes the effective tip smaller than
		// the tip cap.
		var baseFee *uint256.Int
		if rng.IntN(2) == 0 {
			baseFee = uint256.NewInt(pendingTxFeeCap - 1 - rng.Uint64N(3))
		}

		pending := pendingSet(senders)
		got := byPriority(pending, baseFee)
		require.Equalf(t, referenceOrder(pending, baseFee), hashes(t, got), "byPriority() of pool %d", i)
	}
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txgossip

import (
	"bytes"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/utils/heap"
)

// A LazyTransaction couples a [txpool.LazyTransaction] with its sender.
type LazyTransaction struct {
	*txpool.LazyTransaction
	Sender common.Address
}

// TransactionsByPriority calls [txpool.TxPool.Pending] with the given filter
// and merges the results into a single slice. Each sender's transactions stay
// in increasing nonce order. At every position, the next transaction is chosen
// from each sender's lowest remaining nonce: the highest effective gas tip,
// then the earliest first-seen time, then the lowest sender address.
func (s *Set) TransactionsByPriority(filter txpool.PendingFilter) []*LazyTransaction {
	// TODO(arr4n) investigate optimisations; e.g. skipping entire accounts once
	// the block builder has found that a lower-nonced tx is invalid.
	return byPriority(s.Pool.Pending(filter), filter.BaseFee)
}

// byPriority merges per-sender, nonce-ordered transactions with a heap over
// each sender's next transaction, as geth's miner does.
//
// A comparison sort over all of the transactions can't produce this order:
// comparing a sender's own transactions as equal, and everyone else's by tip,
// isn't a strict weak ordering once a sender's tips differ by nonce. The output
// of such a sort then depends on its input order, which comes from ranging over
// a map.
func byPriority(pending map[common.Address][]*txpool.LazyTransaction, baseFee *uint256.Int) []*LazyTransaction {
	var n int
	queues := make([]*senderQueue, 0, len(pending))
	for from, txs := range pending {
		if len(txs) == 0 {
			continue
		}
		n += len(txs)
		q := &senderQueue{sender: from, rest: txs}
		q.advance(baseFee)
		queues = append(queues, q)
	}

	heads := heap.QueueOf(nextBefore, queues...)
	all := make([]*LazyTransaction, 0, n)
	for {
		q, ok := heads.Peek()
		if !ok {
			return all
		}
		all = append(all, q.next)
		if q.advance(baseFee) {
			heads.Fix(0)
		} else {
			_, _ = heads.Pop()
		}
	}
}

// A senderQueue holds one sender's transactions that are yet to be ordered:
// `next`, with its effective gas tip, and the `rest` in nonce order.
type senderQueue struct {
	sender common.Address
	next   *LazyTransaction
	tip    *uint256.Int
	rest   []*txpool.LazyTransaction
}

// advance moves the head of `rest` into `next`, returning false i.f.f. `rest`
// was empty.
func (q *senderQueue) advance(baseFee *uint256.Int) bool {
	if len(q.rest) == 0 {
		return false
	}
	q.next = &LazyTransaction{
		LazyTransaction: q.rest[0],
		Sender:          q.sender,
	}
	q.tip = q.next.effectiveGasTip(baseFee)
	q.rest = q.rest[1:]
	return true
}

// nextBefore reports whether `a`'s next transaction precedes `b`'s.
func nextBefore(a, b *senderQueue) bool {
	if tip := a.tip.Cmp(b.tip); tip != 0 {
		return tip > 0 // Higher tips first
	}
	if !a.next.Time.Equal(b.next.Time) {
		return a.next.Time.Before(b.next.Time)
	}
	return bytes.Compare(a.sender[:], b.sender[:]) < 0
}

// effectiveGasTip is equivalent to [types.Transaction.EffectiveGasTip] but
// assumes that `baseFee` is either nil or <= the transaction's fee cap. This
// assumption avoids the need for [types.ErrGasFeeCapTooLow]. If this invariant
// is broken, effectiveGasTip returns zero.
func (ltx *LazyTransaction) effectiveGasTip(baseFee *uint256.Int) *uint256.Int {
	fee := ltx.GasFeeCap
	tip := ltx.GasTipCap

	if baseFee == nil {
		return tip
	}
	if fee.Cmp(baseFee) <= 0 {
		return new(uint256.Int)
	}

	switch diff := new(uint256.Int).Sub(fee, baseFee); {
	case diff.Cmp(tip) == -1:
		return diff
	default:
		return tip
	}
}

// Resolve shadows the equivalent method on the [txpool.LazyTransaction],
// extending its return signature to include a boolean that is true i.f.f. the
// transaction is non-nil. This avoids the foot-gun of not knowing that a nil
// check needs to be performed.
func (ltx *LazyTransaction) Resolve() (*types.Transaction, bool) {
	tx := ltx.LazyTransaction.Resolve()
	return tx, tx != nil
}

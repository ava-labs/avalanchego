// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txgossip

import (
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

// TransactionsByPriority returns [txpool.TxPool.Pending] by highest effective
// gas tip, then first seen, then sender address. Each sender's transactions stay
// in nonce order.
func (s *Set) TransactionsByPriority(filter txpool.PendingFilter) []*LazyTransaction {
	// TODO(arr4n) investigate optimisations; e.g. skipping entire accounts once
	// the block builder has found that a lower-nonced tx is invalid.

	return byPriority(s.Pool.Pending(filter), filter.BaseFee)
}

// senderTxs is a sender's remaining transactions and the effective gas tip of
// the next one.
type senderTxs struct {
	from common.Address
	txs  []*txpool.LazyTransaction
	tip  *uint256.Int
}

func byPriority(pending map[common.Address][]*txpool.LazyTransaction, baseFee *uint256.Int) []*LazyTransaction {
	var n int
	senders := make([]*senderTxs, 0, len(pending))
	for from, txs := range pending {
		if len(txs) == 0 {
			continue
		}
		n += len(txs)
		senders = append(senders, &senderTxs{
			from: from,
			txs:  txs,
			tip:  effectiveGasTip(txs[0], baseFee),
		})
	}

	// A sort can't respect both tip and nonce order, so merge the senders.
	q := heap.QueueOf(func(a, b *senderTxs) bool {
		if c := a.tip.Cmp(b.tip); c != 0 {
			return c > 0 // Higher tips first
		}
		if c := a.txs[0].Time.Compare(b.txs[0].Time); c != 0 {
			return c < 0
		}
		// Senders are unique, so the order is deterministic.
		return a.from.Cmp(b.from) < 0
	}, senders...)

	all := make([]*LazyTransaction, 0, n)
	for {
		s, ok := q.Peek()
		if !ok {
			return all
		}
		all = append(all, &LazyTransaction{
			LazyTransaction: s.txs[0],
			Sender:          s.from,
		})

		if s.txs = s.txs[1:]; len(s.txs) == 0 {
			q.Pop()
			continue
		}
		s.tip = effectiveGasTip(s.txs[0], baseFee)
		q.Fix(0)
	}
}

// effectiveGasTip is equivalent to [types.Transaction.EffectiveGasTip] but
// assumes that `baseFee` is either nil or <= the transaction's fee cap. This
// assumption avoids the need for [types.ErrGasFeeCapTooLow]. If this invariant
// is broken, effectiveGasTip returns zero.
func effectiveGasTip(ltx *txpool.LazyTransaction, baseFee *uint256.Int) *uint256.Int {
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

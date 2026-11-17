// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txgossip

import (
	"iter"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/vms/evm/topofblock"
)

// A LazyTransaction couples a [txpool.LazyTransaction] with its sender.
type LazyTransaction struct {
	*txpool.LazyTransaction
	Sender common.Address
}

type TopOfBlockAuctionOpen func() bool

func (fn TopOfBlockAuctionOpen) Active() bool {
	return fn != nil
}

func (fn TopOfBlockAuctionOpen) Available() bool {
	return fn()
}

func NoTopOfBlockAuction() TopOfBlockAuctionOpen { return nil }

// TransactionsByPriority yields [txpool.TxPool.Pending] by highest effective
// gas tip, then first seen, then sender address. Each sender's transactions stay
// in nonce order.
func (s *Set) TransactionsByPriority(filter txpool.PendingFilter, auction TopOfBlockAuctionOpen) iter.Seq[*LazyTransaction] {
	// TODO(arr4n) investigate optimisations; e.g. skipping entire accounts once
	// the block builder has found that a lower-nonced tx is invalid.

	return byPriority(s.Pool.Pending(filter), filter.BaseFee, auction)
}

// senderTxs is a sender's remaining transactions and the effective gas tip of
// the first one.
type senderTxs struct {
	from          common.Address
	txs           []*txpool.LazyTransaction
	firstTip      *uint256.Int
	topOfBlockBid *uint256.Int // of txs[0]; nil is no bid
}

func (s *senderTxs) populateFirstTxMetadata(baseFee *uint256.Int, auction TopOfBlockAuctionOpen) {
	s.firstTip = effectiveGasTip(s.txs[0], baseFee)

	s.topOfBlockBid = nil
	if !auction.Active() {
		return
	}
	tx := s.txs[0].Resolve()
	if tx == nil {
		return
	}
	switch bid, ok, err := topofblock.BidOf(tx); {
	case err != nil:
		panic("TODO: log an error here because this is impossible as admission checks")
	case ok:
		s.topOfBlockBid = &bid
	}
}

// outranks reports whether the first transaction of `a` has higher priority
// than the first transaction of `b`.
func (a *senderTxs) outranks(b *senderTxs) bool {
	switch an, bn := a.topOfBlockBid == nil, b.topOfBlockBid == nil; {
	case an && bn:
		// Based on priority only
	case an || bn:
		// Even if zero, any bid outranks any non-bidding tx.
		return !an
	default:
		if c := a.topOfBlockBid.Cmp(b.topOfBlockBid); c != 0 {
			return c > 0 // Higher bids first
		}
	}

	if c := a.firstTip.Cmp(b.firstTip); c != 0 {
		return c > 0 // Higher tips first
	}
	if c := a.txs[0].Time.Compare(b.txs[0].Time); c != 0 {
		return c < 0
	}
	// Senders are unique, so the order is deterministic.
	return a.from.Cmp(b.from) < 0
}

func byPriority(pending map[common.Address][]*txpool.LazyTransaction, baseFee *uint256.Int, auction TopOfBlockAuctionOpen) iter.Seq[*LazyTransaction] {
	return func(yield func(*LazyTransaction) bool) {
		senders := make([]*senderTxs, 0, len(pending))
		for from, txs := range pending {
			if len(txs) == 0 {
				continue
			}

			s := &senderTxs{
				from: from,
				txs:  txs,
			}
			s.populateFirstTxMetadata(baseFee, auction)
			senders = append(senders, s)
		}

		// Heap by first transaction from each sender.
		q := heap.QueueOf((*senderTxs).outranks, senders...)
		for {
			s, ok := q.Peek()
			if !ok {
				return
			}
			if auction.Active() && !auction.Available() && s.topOfBlockBid != nil {
				q.Pop()
				continue
			}
			if !yield(&LazyTransaction{
				LazyTransaction: s.txs[0],
				Sender:          s.from,
			}) {
				return
			}

			s.txs = s.txs[1:]
			if len(s.txs) == 0 {
				q.Pop()
				continue
			}
			s.populateFirstTxMetadata(baseFee, auction)
			q.Fix(0)
		}
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

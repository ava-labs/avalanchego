// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txgossip

import (
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"
)

// A LazyTransaction couples a [txpool.LazyTransaction] with its sender.
type LazyTransaction struct {
	*txpool.LazyTransaction
	Sender common.Address
}

// TransactionsByPriority calls [txpool.TxPool.Pending] with the given filter,
// collapses the results into a slice, and sorts said slice by decreasing gas
// tip then chronologically. Transactions from the same sender are merely sorted
// by increasing nonce.
func (s *Set) TransactionsByPriority(filter txpool.PendingFilter) []*LazyTransaction {
	// TODO(arr4n) investigate optimisations; e.g. skipping entire accounts once
	// the block builder has found that a lower-nonced tx is invalid.

	pending := s.Pool.Pending(filter)
	var n int
	for _, txs := range pending {
		n += len(txs)
	}

	all := make([]*LazyTransaction, n)
	var i int
	for from, txs := range pending {
		for _, tx := range txs {
			all[i] = &LazyTransaction{
				LazyTransaction: tx,
				Sender:          from,
			}
			i++
		}
	}

	slices.SortStableFunc(all, func(a, b *LazyTransaction) int {
		if a.Sender == b.Sender {
			// [txpool.TxPool.Pending] already returns each slice in nonce order
			// and we're performing a stable sort. A direct comparison of nonces
			// would require resolving the lazy transaction.
			return 0
		}

		aTip := a.effectiveGasTip(filter.BaseFee)
		bTip := b.effectiveGasTip(filter.BaseFee)
		if tip := aTip.Cmp(bTip); tip != 0 {
			return -tip // Higher tips first
		}

		return a.Time.Compare(b.Time)
	})
	return all
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

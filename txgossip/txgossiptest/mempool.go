// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package txgossiptest provides test helpers for mempool operations.
package txgossiptest

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"
)

// WaitUntilPending waits until all transactions provided are marked as pending in `pool`.
func WaitUntilPending(tb testing.TB, ctx context.Context, pool *txpool.TxPool, txs ...*types.Transaction) {
	tb.Helper()

	if len(txs) == 0 {
		return
	}

	txCh := make(chan core.NewTxsEvent, 1) // size arbitrary
	sub := pool.SubscribeTransactions(txCh, true /*reorgs but ignored by legacypool*/)
	defer sub.Unsubscribe()

	s := set.NewSet[common.Hash](len(txs))
	for _, tx := range txs {
		s.Add(tx.Hash())
	}

	// Optimistically check current mempool - any reorgs after this will
	// certainly be caught by the subscription.
	pendingByAddr, _ := pool.Content()
	for _, list := range pendingByAddr {
		for _, tx := range list {
			s.Remove(tx.Hash())
		}
	}

	if s.Len() == 0 {
		// already found all txs
		return
	}

	for {
		select {
		case <-ctx.Done():
			tb.Fatalf("%v waiting for %T.SubscribeTransactions()", context.Cause(ctx), pool)
		case err := <-sub.Err():
			tb.Fatalf("%T.SubscribeTransactions.Err() returned %v", pool, err)
		case txEvent := <-txCh:
			for _, tx := range txEvent.Txs {
				s.Remove(tx.Hash())
			}

			if s.Len() == 0 {
				return
			}
		}
	}
}

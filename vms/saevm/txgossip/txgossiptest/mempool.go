// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package txgossiptest provides test helpers for mempool operations.
package txgossiptest

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/set"
)

// Mempool is the subset of mempool behaviour [WaitUntilPending] needs: a
// subscription to new-transaction events and a snapshot of the currently
// pending transactions. It is satisfied directly by geth's RPC backend.
type Mempool interface {
	SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription
	GetPoolTransactions() (types.Transactions, error)
}

// WaitUntilPending waits until all transactions provided are marked as pending
// in mempool.
func WaitUntilPending(tb testing.TB, ctx context.Context, mempool Mempool, txs ...*types.Transaction) {
	tb.Helper()

	if len(txs) == 0 {
		return
	}

	txCh := make(chan core.NewTxsEvent, 1) // size arbitrary
	sub := mempool.SubscribeNewTxsEvent(txCh)
	defer sub.Unsubscribe()

	want := set.NewSet[common.Hash](len(txs))
	for _, tx := range txs {
		want.Add(tx.Hash())
	}

	for {
		// Subscribing before the snapshot guarantees no promotion is missed,
		// so each wakeup just re-derives the pending set from the authoritative
		// source rather than tracking add-events that may be queued, not pending.
		pending, err := mempool.GetPoolTransactions()
		require.NoErrorf(tb, err, "%T.GetPoolTransactions()", mempool)
		for _, tx := range pending {
			want.Remove(tx.Hash())
		}
		if want.Len() == 0 {
			return
		}

		select {
		case <-ctx.Done():
			tb.Fatalf("%v waiting for %d pending txs %x in %T", context.Cause(ctx), want.Len(), want.List(), mempool)
		case err := <-sub.Err():
			tb.Fatalf("%T.SubscribeNewTxsEvent.Err() returned %v", mempool, err)
		case <-txCh:
		}
	}
}

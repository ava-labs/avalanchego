// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package txgossiptest provides test helpers for mempool operations.
package txgossiptest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/utils/set"
)

// WaitUntilPending waits until all transactions provided are marked as pending in `pool`.
func WaitUntilPending(tb testing.TB, ctx context.Context, pool *txpool.TxPool, txs ...*types.Transaction) {
	tb.Helper()

	if len(txs) == 0 {
		return
	}
	marshaled, err := json.MarshalIndent(txs[0], "", "  ")
	if err != nil {
		tb.Fatalf("marshal failed: %v", err)
	}
	tb.Logf("tx:\n%s", marshaled)

	txCh := make(chan core.NewTxsEvent, 1) // size arbitrary
	sub := pool.SubscribeTransactions(txCh, true /*reorgs but ignored by legacypool*/)
	defer sub.Unsubscribe()

	s := set.NewSet[common.Hash](len(txs))
	for _, tx := range txs {
		s.Add(tx.Hash())
	}

	check := func() {
		// Optimistically check current mempool - any reorgs after this will
		// certainly be caught by the subscription.
		pendingByAddr, _ := pool.Content()
		for _, list := range pendingByAddr {
			for _, tx := range list {
				s.Remove(tx.Hash())
			}
		}
	}
	check()

	if s.Len() == 0 {
		// already found all txs
		tb.Logf("found tx first try: %+x", txs[0].Hash())
		return
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
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
		case <-ticker.C:
			tb.Logf("still waiting for %T.SubscribeTransactions(): %+x", pool, txs[0].Hash())
			check()
			if s.Len() == 0 {
				tb.Log("exiting with ticker")
				return
			}
		}
	}
}

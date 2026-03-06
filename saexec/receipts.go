// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"cmp"
	"context"
	"runtime"
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/sync"

	"github.com/ava-labs/strevm/blocks"
)

var receiptChPool = sync.Pool[chan *Receipt]{
	New: func() chan *Receipt { return make(chan *Receipt, 1) },
}

func (e *Executor) createReceiptBuffers(b *blocks.Block) {
	txs := make([]common.Hash, len(b.Transactions()))
	for i, tx := range b.Transactions() {
		txs[i] = tx.Hash()
	}
	e.receipts.StoreFromFunc(func(common.Hash) chan *Receipt {
		return receiptChPool.Get()
	}, txs...)
	// This satisfies the minimum-lifespan guarantee of [Executor.RecentReceipt]
	// but, in practice, will keep the receipts around until the block is an
	// ancestor of the last-settled block. See [sae.VM.AcceptBlock] for details.
	// This should adequately cover the post-tx-issuance period during which
	// users request receipts.
	runtime.AddCleanup(b, func(rs *syncMap[common.Hash, chan *Receipt]) {
		for _, tx := range txs {
			if ch, ok := rs.Load(tx); ok {
				select {
				case <-ch:
				default:
				}
				receiptChPool.Put(ch)
			}
		}
		rs.Delete(txs...)
	}, e.receipts)
}

// A Receipt couples a [types.Receipt] with its respective [types.Transaction]
// and the [types.Signer] used to determine the transaction sender. Together
// these provide all information necessary to marshal the receipt for an RPC
// response.
type Receipt struct {
	*types.Receipt
	Signer types.Signer
	Tx     *types.Transaction
}

// RecentReceipt returns the receipt for the specified [types.Transaction] hash,
// as soon as it is ready for issuance, even if later transactions in the same
// block are still executing. It caches recent values for an indefinite period,
// with the only guarantee being that the receipt will be written to disk before
// it is cleared from the cache. This allows for fallback to a standard
// receipt-fetching mechanism.
//
// If the transaction has been included in a block passed to [Executor.Enqueue]
// then RecentReceipt will block until the transaction has executed, while
// honouring context cancellation. In this case, the returned boolean will be
// true.
//
// If the returned boolean is false then the [Executor] has no knowledge of the
// transaction, either because it hasn't been enqueued, or because it has been
// cleared from the cache.
//
// The only possible error is one returned by [context.Cause] upon context
// cancellation.
func (e *Executor) RecentReceipt(ctx context.Context, tx common.Hash) (*Receipt, bool, error) {
	ch, ok := e.receipts.Load(tx)
	if !ok {
		return nil, false, nil
	}
	// TODO(arr4n) abstract the internal libevm/parallel `eventual` type into a
	// separate package and use it here with `peek()`. It's semantically
	// identical but clearer than receiving and then immediately sending a value
	// with a single-buffered channel.
	select {
	case r := <-ch:
		ch <- r
		return r, true, nil
	case <-ctx.Done():
		return nil, true, context.Cause(ctx)
	}
}

// A syncMap holds values keyed by uniformly distributed keys, allowing for
// reduced lock contention. The distribution of keys is a required property, not
// one provided by the map.
type syncMap[K ~[32]byte, V any] struct {
	buckets [256]bucket[K, V]
}

type bucket[K comparable, V any] struct {
	sync.RWMutex
	data map[K]V
}

func newSyncMap[K ~[32]byte, V any]() *syncMap[K, V] {
	m := new(syncMap[K, V])
	for i := range m.buckets {
		m.buckets[i].data = make(map[K]V)
	}
	return m
}

// writeMany calls `fn` once for each `K`, also providing the respective
// [bucket] map while holding said bucket's lock for writing.
//
// writeMany is not atomic across all keys, but it is thread-safe. It is instead
// optimised such that the worst-case number of locks taken is bounded by the
// number of buckets, not the number of keys. To achieve this, the caller MUST
// consider the order in which keys are passed to `fn` to be undefined.
func (m *syncMap[K, V]) writeMany(fn func(map[K]V, K), ks ...K) {
	slices.SortFunc(ks, func(a, b K) int {
		return cmp.Compare(a[0], b[0])
	})

	var b *bucket[K, V]
	unlock := func() {
		if b != nil {
			b.Unlock()
		}
	}
	for _, k := range ks {
		if next := &m.buckets[k[0]]; next != b {
			unlock()
			next.Lock()
			b = next
		}
		fn(b.data, k)
	}
	unlock()
}

func (m *syncMap[K, V]) StoreFromFunc(fn func(K) V, ks ...K) {
	m.writeMany(func(m map[K]V, k K) {
		m[k] = fn(k)
	}, ks...)
}

func (m *syncMap[K, V]) Delete(ks ...K) {
	m.writeMany(func(m map[K]V, k K) {
		delete(m, k)
	}, ks...)
}

func (m *syncMap[K, V]) Load(k K) (V, bool) {
	b := &m.buckets[k[0]]
	b.RLock()
	v, ok := b.data[k]
	b.RUnlock()
	return v, ok
}

// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gasprice

import (
	"math/big"
	"slices"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rpc"
	"go.uber.org/zap"
)

type transaction struct {
	gas uint64   // [types.Transaction.Gas]
	tip *big.Int // [types.Transaction.EffectiveGasTip]
}

func newTx(tx *types.Transaction, baseFee *big.Int) transaction {
	return transaction{
		tx.Gas(),
		tx.EffectiveGasTipValue(baseFee),
	}
}

func (t transaction) Compare(o transaction) int {
	return t.tip.Cmp(o.tip)
}

type block struct {
	timestamp uint64        // [types.Header.Time]
	gasUsed   uint64        // [types.Header.GasUsed]
	gasLimit  uint64        // [types.Header.GasLimit]
	baseFee   *big.Int      // [types.Header.BaseFee]
	txs       []transaction // sorted ascending by tip
}

func newBlock(blk *types.Block) *block {
	txs := blk.Transactions()
	b := &block{
		timestamp: blk.Time(),
		gasUsed:   blk.GasUsed(),
		gasLimit:  blk.GasLimit(),
		baseFee:   blk.BaseFee(),
		txs:       make([]transaction, len(txs)),
	}
	if b.baseFee == nil {
		b.baseFee = new(big.Int)
	}
	for i, tx := range txs {
		b.txs[i] = newTx(tx, b.baseFee)
	}
	slices.SortFunc(b.txs, transaction.Compare)
	return b
}

// tipPercentiles computes the gas-weighted tip at each requested percentile.
// all of which MUST be sorted in ascending order.
//
// Because block builders sequence transactions without executing them in SAE,
// we accumulate gas limits, not the gas charged.
func (b *block) tipPercentiles(percentiles []float64) []*big.Int {
	out := make([]*big.Int, len(percentiles))
	if len(b.txs) == 0 {
		for i := range out {
			out[i] = new(big.Int)
		}
		return out
	}

	var (
		txIndex = 0
		sumGas  = b.txs[0].gas
	)
	for i, p := range percentiles {
		threshold := uint64(float64(b.gasUsed) * p / 100)
		// TODO:(StephenButtolph): Improve from `O(txs + percentiles)` to
		// `O(percentiles * log(txs))` by binary searching for each threshold if
		// networks with large blocks encounter performance degradation.
		for sumGas < threshold && txIndex < len(b.txs)-1 {
			txIndex++
			sumGas += b.txs[txIndex].gas
		}
		out[i] = b.txs[txIndex].tip
	}
	return out
}

type blockCache struct {
	log     logging.Logger
	backend Backend
	// TODO(StephenButtolph): Use a ring-buffer rather than an LRU cache if we
	// observe cache contention in production.
	cache *lru.Cache[uint64, *block]
}

func newBlockCache(log logging.Logger, backend Backend, size int) *blockCache {
	return &blockCache{
		log:     log,
		backend: backend,
		cache:   lru.NewCache[uint64, *block](size),
	}
}

// getBlock returns the block at height n. If the block does not exist, it will
// return nil.
func (b *blockCache) getBlock(n uint64) *block {
	if blk, ok := b.cache.Get(n); ok {
		return blk
	}

	blk, err := b.backend.BlockByNumber(rpc.BlockNumber(n)) //nolint:gosec // block numbers were previously resolved
	if err != nil {
		b.log.Error("fetching BlockByNumber",
			zap.Uint64("number", n),
			zap.Error(err),
		)
		return nil
	}
	// Don't cache a nil block. It may be populated in the future.
	if blk == nil {
		return nil
	}
	return b.cacheBlock(blk)
}

func (b *blockCache) cacheBlock(blk *types.Block) *block {
	newB := newBlock(blk)
	b.cache.Put(blk.NumberU64(), newB)
	return newB
}

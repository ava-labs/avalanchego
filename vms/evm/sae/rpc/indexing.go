// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"io"
	"math"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/lru"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/bloombits"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/eth"
	"github.com/ava-labs/libevm/eth/filters"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
)

// chainIndexer implements the subset of [ethapi.Backend] required to back a
// [core.ChainIndexer].
type chainIndexer struct {
	Chain
}

var _ core.ChainIndexerChain = chainIndexer{}

func (c chainIndexer) CurrentHeader() *types.Header {
	return c.LastExecuted().Header()
}

// A bloomOverrider constructs Bloom filters from persisted receipts instead of
// relying on the [types.Header] field.
type bloomOverrider struct {
	chain Chain
}

var _ filters.BloomOverrider = bloomOverrider{}

// OverrideHeaderBloom returns the Bloom filter of the receipts generated when
// executing the respective block, whereas the [types.Header] carries those
// settled by the block.
func (b bloomOverrider) OverrideHeaderBloom(header *types.Header) types.Bloom {
	return types.CreateBloom(rawdb.ReadRawReceipts(
		b.chain.DB(),
		header.Hash(),
		header.Number.Uint64(),
	))
}

// bloomCacheSize is the number of Bloom filters that a [cachedBloomOverrider]
// keeps, each of them [types.BloomByteLength] bytes.
const bloomCacheSize = 2048

// A cachedBloomOverrider is a [bloomOverrider] for log queries, which check the
// same recent blocks repeatedly. A [bloomIndexer] SHOULD NOT use it because it
// reads every block of a section once, evicting the blocks worth keeping.
type cachedBloomOverrider struct {
	chain Chain
	cache *lru.Cache[common.Hash, types.Bloom]
}

func newCachedBloomOverrider(chain Chain) cachedBloomOverrider {
	return cachedBloomOverrider{
		chain: chain,
		cache: lru.NewCache[common.Hash, types.Bloom](bloomCacheSize),
	}
}

var _ filters.BloomOverrider = cachedBloomOverrider{}

// OverrideHeaderBloom is equivalent to [bloomOverrider.OverrideHeaderBloom].
func (b cachedBloomOverrider) OverrideHeaderBloom(header *types.Header) types.Bloom {
	hash := header.Hash()
	if bloom, ok := b.cache.Get(hash); ok {
		return bloom
	}
	receipts := rawdb.ReadRawReceipts(b.chain.DB(), hash, header.Number.Uint64())
	bloom := types.CreateBloom(receipts)
	// Receipts are written when a block is executed, so nil receipts only mean
	// that the Bloom filter isn't known yet. A block without logs has non-nil
	// receipts.
	if receipts != nil {
		b.cache.Add(hash, bloom)
	}
	return bloom
}

// bloomIndexer provides the [bloomIndexer.BloomStatus] and
// [bloomIndexer.ServiceFilter] methods of an [ethapi.Backend] implementation.
type bloomIndexer struct {
	indexer  *core.ChainIndexer
	size     uint64
	handlers *eth.BloomHandlers
}

// newBloomIndexer creates a [bloomIndexer] and starts the indexer to run with
// events from `chain`.
//
// The consumer must call [bloomIndexer.Close] to release allocated resources.
func newBloomIndexer(db ethdb.Database, chain core.ChainIndexerChain, override filters.BloomOverrider, size uint64) *bloomIndexer {
	if size == 0 || size > math.MaxInt32 {
		size = params.BloomBitsBlocks
	}

	backend := &bloomBackend{
		BloomIndexer:   core.NewBloomIndexerBackend(db, size),
		BloomOverrider: override,
	}
	table := rawdb.NewTable(db, string(rawdb.BloomBitsIndexPrefix))
	b := &bloomIndexer{
		indexer:  core.NewChainIndexer(db, table, backend, size, 0, core.BloomThrottling, "bloombits"),
		size:     size,
		handlers: eth.StartBloomHandlers(db, size),
	}
	b.indexer.Start(chain)
	return b
}

func (b *bloomIndexer) BloomStatus() (size uint64, sections uint64) {
	sections, _, _ = b.indexer.Sections()
	return b.size, sections
}

func (b *bloomIndexer) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for range eth.BloomFilterThreads {
		go session.Multiplex(eth.BloomRetrievalBatch, eth.BloomRetrievalWait, b.handlers.Requests)
	}
}

var _ io.Closer = (*bloomIndexer)(nil)

func (b *bloomIndexer) Close() error {
	b.handlers.Close()
	return b.indexer.Close()
}

var _ core.ChainIndexerBackend = (*bloomBackend)(nil)

// bloomBackend is a wrapper around a [core.BloomIndexer] that overrides
// Process() to allow for custom bloom-filter generation.
type bloomBackend struct {
	*core.BloomIndexer
	filters.BloomOverrider
}

func (b *bloomBackend) Process(ctx context.Context, hdr *types.Header) error {
	return b.ProcessWithBloomOverride(hdr, b.OverrideHeaderBloom(hdr))
}

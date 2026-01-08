// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lru

import (
	"hash/fnv"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	// defaultNumShards is the default number of shards for the cache.
	// 16 shards provides a good balance between lock contention reduction
	// and memory overhead. With 16 shards, lock contention is reduced by ~16x.
	defaultNumShards = 16
)

var _ cache.Cacher[struct{}, struct{}] = (*ShardedCache[struct{}, struct{}])(nil)

// ShardedCache is a thread-safe LRU cache that uses multiple internal caches
// (shards) to reduce lock contention. Keys are distributed across shards
// using a hash function, allowing concurrent operations on different shards.
type ShardedCache[K comparable, V any] struct {
	shards    []*Cache[K, V]
	numShards int
}

// NewShardedCache creates a new sharded LRU cache with the given total size.
// The size is divided equally among all shards.
func NewShardedCache[K comparable, V any](size int) *ShardedCache[K, V] {
	return NewShardedCacheWithOptions(size, defaultNumShards, func(K, V) {})
}

// NewShardedCacheWithOnEvict creates a new sharded LRU cache with the given
// total size and eviction callback.
func NewShardedCacheWithOnEvict[K comparable, V any](size int, onEvict func(K, V)) *ShardedCache[K, V] {
	return NewShardedCacheWithOptions(size, defaultNumShards, onEvict)
}

// NewShardedCacheWithOptions creates a new sharded LRU cache with full configuration.
// size: total cache size (divided equally among shards)
// numShards: number of internal cache shards (must be > 0)
// onEvict: callback function called before evicting an entry
func NewShardedCacheWithOptions[K comparable, V any](size, numShards int, onEvict func(K, V)) *ShardedCache[K, V] {
	if numShards <= 0 {
		numShards = defaultNumShards
	}

	// Divide total size among shards, ensuring each shard has at least size 1
	shardSize := max(size/numShards, 1)

	shards := make([]*Cache[K, V], numShards)
	for i := 0; i < numShards; i++ {
		shards[i] = NewCacheWithOnEvict(shardSize, onEvict)
	}

	return &ShardedCache[K, V]{
		shards:    shards,
		numShards: numShards,
	}
}

// getShard returns the shard index for a given key using FNV-1a hash.
// FNV-1a provides good distribution and is fast for short keys.
func (sc *ShardedCache[K, V]) getShard(key K) *Cache[K, V] {
	// Hash the key to determine shard
	h := fnv.New64a()

	// Convert key to bytes using the hashing utility
	// This handles various comparable types (string, int, struct, etc.)
	keyBytes := hashing.ComputeHash256(key)
	h.Write(keyBytes[:])

	// Use modulo to get shard index
	shardIdx := h.Sum64() % uint64(sc.numShards)
	return sc.shards[shardIdx]
}

// Put adds or updates a key-value pair in the cache.
// If the cache is at capacity, the least recently used item in the
// corresponding shard will be evicted before insertion.
func (sc *ShardedCache[K, V]) Put(key K, value V) {
	shard := sc.getShard(key)
	shard.Put(key, value)
}

// Get retrieves the value for a given key and marks it as recently used.
// Returns the value and true if found, or zero value and false if not found.
func (sc *ShardedCache[K, V]) Get(key K) (V, bool) {
	shard := sc.getShard(key)
	return shard.Get(key)
}

// Evict removes a key from the cache if it exists.
func (sc *ShardedCache[K, V]) Evict(key K) {
	shard := sc.getShard(key)
	shard.Evict(key)
}

// Flush removes all entries from all shards, calling the eviction callback
// for each entry.
func (sc *ShardedCache[K, V]) Flush() {
	for _, shard := range sc.shards {
		shard.Flush()
	}
}

// Len returns the total number of entries across all shards.
// Note: This iterates through all shards and may be slower than a single-threaded cache.
func (sc *ShardedCache[K, V]) Len() int {
	total := 0
	for _, shard := range sc.shards {
		total += shard.Len()
	}
	return total
}

// PortionFilled returns the average fill ratio across all shards.
// A value of 1.0 means the cache is completely full.
func (sc *ShardedCache[K, V]) PortionFilled() float64 {
	totalFilled := 0.0
	for _, shard := range sc.shards {
		totalFilled += shard.PortionFilled()
	}
	return totalFilled / float64(sc.numShards)
}

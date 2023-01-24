// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

// Cacher acts as a best effort key value store.
type Cacher[K comparable, V any] interface {
	// Put inserts an element into the cache. If space is required, elements will
	// be evicted.
	Put(key K, value V)

	// Get returns the entry in the cache with the key specified, if no value
	// exists, false is returned.
	Get(key K) (V, bool)

	// Evict removes the specified entry from the cache
	Evict(key K)

	// Flush removes all entries from the cache
	Flush()
}

// Evictable allows the object to be notified when it is evicted
type Evictable[K comparable] interface {
	Key() K
	Evict()
}

// Deduplicator acts as a best effort deduplication service
type Deduplicator[K comparable, V Evictable[K]] interface {
	// Deduplicate returns either the provided value, or a previously provided
	// value with the same ID that hasn't yet been evicted
	Deduplicate(V) V

	// Flush removes all entries from the cache
	Flush()
}

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

// Cacher acts as a best effort key value store. Keys must be comparable, as
// defined by https://golang.org/ref/spec#Comparison_operators.
type Cacher[T comparable, K any] interface {
	// Put inserts an element into the cache. If spaced is required, elements will
	// be evicted.
	Put(key T, value K)

	// Get returns the entry in the cache with the key specified, if no value
	// exists, false is returned.
	Get(key T) (K, bool)

	// Evict removes the specified entry from the cache
	Evict(key T)

	// Flush removes all entries from the cache
	Flush()
}

// Evictable allows the object to be notified when it is evicted
type Evictable[T comparable] interface {
	Key() T
	Evict()
}

// Deduplicator acts as a best effort deduplication service
type Deduplicator[T comparable, K Evictable[T]] interface {
	// Deduplicate returns either the provided value, or a previously provided
	// value with the same ID that hasn't yet been evicted
	Deduplicate(Evictable[T]) Evictable[T]

	// Flush removes all entries from the cache
	Flush()
}

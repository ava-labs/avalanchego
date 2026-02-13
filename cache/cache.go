// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

	// Returns the number of elements currently in the cache
	Len() int

	// Returns fraction of cache currently filled (0 --> 1)
	PortionFilled() float64
}

//go:build cgo && !windows
// +build cgo,!windows

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"github.com/ava-labs/firewood-go-ethhash/ffi"
)

const (
	// DefaultCacheSizeBytes is the default size for Firewood's cache
	// Merkle trie databases benefit from caching frequently accessed nodes
	DefaultCacheSizeBytes = 512 * 1024 * 1024 // 512 MB

	// DefaultFreeListCacheEntries is the default number of free list entries to cache
	// Firewood uses a free list for memory management
	DefaultFreeListCacheEntries = 1024

	// DefaultRevisionsInMemory is the default number of historical revisions to keep
	// Firewood supports versioned storage - this controls memory vs disk trade-off
	DefaultRevisionsInMemory = 10

	// DefaultReadCacheSize is the default number of key-value pairs to cache in the
	// Go-level read cache. This cache sits in front of the FFI layer and serves hot
	// reads (e.g. repeated Get/ParallelGet of the same committed keys) without any
	// trie traversal or CGO overhead. The cache is cleared on every flush/commit so
	// it never returns stale data for writes that have been applied to the trie.
	DefaultReadCacheSize = 4096
)

// Config defines configuration options for Firewood database
//
// Firewood is a merkle trie database optimized for blockchain state storage.
// It provides:
// - Built-in merkle proof generation
// - Versioned storage (historical state queries)
// - Efficient trie pruning
// - Memory-mapped I/O for performance
type Config struct {
	// CacheSizeBytes controls the size of the in-memory node cache
	// Larger values improve read performance but increase memory usage
	// Recommended: 512 MB - 2 GB depending on available RAM
	CacheSizeBytes uint `json:"cacheSizeBytes"`

	// FreeListCacheEntries controls free list caching for allocation efficiency
	// Higher values reduce allocation overhead at cost of memory
	// Recommended: 1024 - 4096
	FreeListCacheEntries uint `json:"freeListCacheEntries"`

	// RevisionsInMemory controls how many historical revisions to keep in memory
	// Higher values allow faster historical queries but increase memory usage
	// Set to 0 to disable historical queries (lowest memory usage)
	// Recommended: 10 for most use cases, 0 for constrained systems
	RevisionsInMemory uint `json:"revisionsInMemory"`

	// CacheStrategy determines eviction policy for the node cache
	// Uses FFI CacheStrategy type from firewood-go-ethhash
	CacheStrategy ffi.CacheStrategy `json:"cacheStrategy"`

	// FlushSize controls auto-flush threshold for pending writes
	// When pending operations reach this count, they are automatically committed
	// Higher values = better batch efficiency but more memory
	// Lower values = lower memory but more frequent commits
	// Recommended: 1000 for most use cases
	FlushSize int `json:"flushSize"`

	// ReadCacheSize controls the maximum number of entries in the Go-level read cache.
	// The cache stores recently-read committed key-value pairs in Go memory, bypassing
	// the FFI trie traversal for hot keys. It is cleared on every flush/batch-commit, so
	// pending writes always take priority via the pending-batch fast path in Get().
	// Set to 0 to disable the read cache.
	// Recommended: 4096 (covers typical blockchain hot-set with negligible memory)
	ReadCacheSize int `json:"readCacheSize"`

	// RootStore enables persisting historical revisions to disk.
	// Without this, revisions only exist in memory and are lost on restart.
	// CRITICAL: Must be true for data to survive process restarts.
	// When enabled, Firewood uses a root_store/ subdirectory to persist
	// revision history, allowing the database to recover its state on restart.
	RootStore bool `json:"rootStore"`
}

// DefaultConfig returns the default Firewood configuration
func DefaultConfig() Config {
	return Config{
		CacheSizeBytes:       DefaultCacheSizeBytes,
		FreeListCacheEntries: DefaultFreeListCacheEntries,
		RevisionsInMemory:    DefaultRevisionsInMemory,
		CacheStrategy:        ffi.CacheAllReads,
		FlushSize:            DefaultFlushSize,
		RootStore:            false, // Historical revisions not persisted; saves disk space
		ReadCacheSize:        DefaultReadCacheSize,
	}
}

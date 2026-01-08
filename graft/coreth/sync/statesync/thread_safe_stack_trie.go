// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/trie"
)

// ThreadSafeStackTrie wraps trie.StackTrie with mutex protection
// to enable safe concurrent access from multiple goroutines.
//
// StackTrie from libevm has no internal synchronization and requires
// keys to be inserted in ascending order. This wrapper serializes
// all operations to maintain correctness.
type ThreadSafeStackTrie struct {
	mu        sync.Mutex
	stackTrie *trie.StackTrie
}

// NewThreadSafeStackTrie creates a new thread-safe wrapper around StackTrie.
func NewThreadSafeStackTrie(options *trie.StackTrieOptions) *ThreadSafeStackTrie {
	return &ThreadSafeStackTrie{
		stackTrie: trie.NewStackTrie(options),
	}
}

// Update inserts a key-value pair into the trie in a thread-safe manner.
// Keys must still be provided in ascending order across all goroutines.
// This method serializes access to the underlying StackTrie.
func (t *ThreadSafeStackTrie) Update(key []byte, value []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stackTrie.Update(key, value)
}

// Commit finalizes the trie and returns the root hash in a thread-safe manner.
// This method serializes access to the underlying StackTrie.
func (t *ThreadSafeStackTrie) Commit() common.Hash {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stackTrie.Commit()
}

// Reset clears the trie to empty state in a thread-safe manner.
// This method serializes access to the underlying StackTrie.
func (t *ThreadSafeStackTrie) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stackTrie.Reset()
}

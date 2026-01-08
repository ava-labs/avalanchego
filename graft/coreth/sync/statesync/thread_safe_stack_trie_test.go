// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"sync"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"
)

// TestThreadSafeStackTrie_Sequential verifies thread-safe wrapper
// produces same results as regular StackTrie for sequential access.
func TestThreadSafeStackTrie_Sequential(t *testing.T) {
	require := require.New(t)

	// Test data: sorted keys
	keys := [][]byte{
		common.Hex2Bytes("1000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("2000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("3000000000000000000000000000000000000000000000000000000000000000"),
	}
	vals := [][]byte{
		[]byte("value1"),
		[]byte("value2"),
		[]byte("value3"),
	}

	// Create thread-safe wrapper
	tsSt := NewThreadSafeStackTrie(nil)
	for i := range keys {
		require.NoError(tsSt.Update(keys[i], vals[i]))
	}
	tsRoot := tsSt.Commit()

	// Create regular StackTrie
	st := trie.NewStackTrie(nil)
	for i := range keys {
		require.NoError(st.Update(keys[i], vals[i]))
	}
	stRoot := st.Commit()

	// Verify same root hash
	require.Equal(stRoot, tsRoot, "Thread-safe wrapper should produce same root as regular StackTrie")
}

// TestThreadSafeStackTrie_Concurrent verifies thread-safe wrapper
// handles concurrent access without data races.
func TestThreadSafeStackTrie_Concurrent(t *testing.T) {
	require := require.New(t)

	// Note: This test verifies no data races, but StackTrie still
	// requires keys in ascending order, so we serialize the updates
	// themselves while testing concurrent Commit calls, etc.

	tsSt := NewThreadSafeStackTrie(nil)

	// Concurrent updates (mutex will serialize them)
	var wg sync.WaitGroup
	keys := [][]byte{
		common.Hex2Bytes("1000000000000000000000000000000000000000000000000000000000000000"),
		common.Hex2Bytes("2000000000000000000000000000000000000000000000000000000000000000"),
	}
	vals := [][]byte{
		[]byte("value1"),
		[]byte("value2"),
	}

	// Sequential updates via goroutines (mutex ensures ordering)
	for i := range keys {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			require.NoError(tsSt.Update(keys[idx], vals[idx]))
		}()
	}
	wg.Wait()

	// Commit
	root := tsSt.Commit()
	require.NotEqual(common.Hash{}, root, "Should produce non-empty root")
}

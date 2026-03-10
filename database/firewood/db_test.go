// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build cgo && !windows
// +build cgo,!windows

package firewood

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// TestFirewoodBasicOperations tests basic Put/Get/Delete operations
func TestFirewoodBasicOperations(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "firewood-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	log := logging.NoLog{}
	db, err := New(dbPath, nil, log)
	require.NoError(err)
	require.NotNil(db)
	defer db.Close()

	// Test Put
	key := []byte("test-key")
	value := []byte("test-value")
	err = db.Put(key, value)
	require.NoError(err)

	// Test Has (pending batch)
	has, err := db.Has(key)
	require.NoError(err)
	require.True(has, "should find key in pending batch")

	// Test Get (pending batch - read-your-writes)
	retrieved, err := db.Get(key)
	require.NoError(err)
	require.Equal(value, retrieved)

	// Flush pending
	fwDB := db.(*Database)
	fwDB.pendingMu.Lock()
	err = fwDB.flushLocked()
	fwDB.pendingMu.Unlock()
	require.NoError(err)

	// Test Get (committed state)
	retrieved, err = db.Get(key)
	require.NoError(err)
	require.Equal(value, retrieved)

	// Test Delete
	err = db.Delete(key)
	require.NoError(err)

	// Test Has (should see delete in pending)
	has, err = db.Has(key)
	require.NoError(err)
	require.False(has)

	// Test Get (should return not found)
	_, err = db.Get(key)
	require.ErrorIs(err, database.ErrNotFound)
}

// TestFirewoodAutoFlush tests auto-flush behavior
func TestFirewoodAutoFlush(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "firewood-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	log := logging.NoLog{}
	db, err := New(dbPath, nil, log)
	require.NoError(err)
	defer db.Close()

	fwDB := db.(*Database)
	fwDB.flushSize = 10 // Auto-flush at 10 ops

	// Add 9 operations - should not flush
	for i := 0; i < 9; i++ {
		key := []byte{byte(i)}
		value := []byte{byte(i * 2)}
		err = db.Put(key, value)
		require.NoError(err)
	}

	// Verify pending batch has 9 ops
	fwDB.pendingMu.Lock()
	pendingCount := len(fwDB.pending.ops)
	fwDB.pendingMu.Unlock()
	require.Equal(9, pendingCount)

	// Add 10th operation - should trigger flush
	key10 := []byte{10}
	value10 := []byte{20}
	err = db.Put(key10, value10)
	require.NoError(err)

	// Verify pending batch was flushed
	fwDB.pendingMu.Lock()
	pendingCount = len(fwDB.pending.ops)
	fwDB.pendingMu.Unlock()
	require.Equal(0, pendingCount, "pending batch should be empty after auto-flush")

	// Verify data was committed
	retrieved, err := db.Get(key10)
	require.NoError(err)
	require.Equal(value10, retrieved)
}

// TestFirewoodBatch tests batch operations
func TestFirewoodBatch(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "firewood-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	log := logging.NoLog{}
	db, err := New(dbPath, nil, log)
	require.NoError(err)
	defer db.Close()

	// Create batch
	batch := db.NewBatch()

	// Add operations to batch
	key1 := []byte("key1")
	value1 := []byte("value1")
	err = batch.Put(key1, value1)
	require.NoError(err)

	key2 := []byte("key2")
	value2 := []byte("value2")
	err = batch.Put(key2, value2)
	require.NoError(err)

	// Batch should have size
	size := batch.Size()
	require.Greater(size, 0)

	// Write batch atomically
	err = batch.Write()
	require.NoError(err)

	// Verify both keys exist
	retrieved1, err := db.Get(key1)
	require.NoError(err)
	require.Equal(value1, retrieved1)

	retrieved2, err := db.Get(key2)
	require.NoError(err)
	require.Equal(value2, retrieved2)

	// Test batch reset
	batch.Reset()
	require.Equal(0, batch.Size())
}

// TestFirewoodCloseFlush tests that Close() flushes pending writes
func TestFirewoodCloseFlush(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "firewood-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	log := logging.NoLog{}
	db, err := New(dbPath, nil, log)
	require.NoError(err)

	// Add data to pending batch
	key := []byte("flush-on-close")
	value := []byte("should-be-committed")
	err = db.Put(key, value)
	require.NoError(err)

	// Close should flush pending
	err = db.Close()
	require.NoError(err)

	// Reopen database
	db2, err := New(dbPath, nil, log)
	require.NoError(err)
	defer db2.Close()

	// Verify data was committed
	retrieved, err := db2.Get(key)
	require.NoError(err)
	require.Equal(value, retrieved)
}

// TestFirewoodIterator tests basic iterator functionality
func TestFirewoodIterator(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "firewood-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	log := logging.NoLog{}
	db, err := New(dbPath, nil, log)
	require.NoError(err)
	defer db.Close()

	// Add some data and flush
	testData := map[string]string{
		"apple":  "fruit",
		"banana": "fruit",
		"carrot": "vegetable",
	}

	for key, value := range testData {
		err = db.Put([]byte(key), []byte(value))
		require.NoError(err)
	}

	// Flush to committed state
	fwDB := db.(*Database)
	fwDB.pendingMu.Lock()
	err = fwDB.flushLocked()
	fwDB.pendingMu.Unlock()
	require.NoError(err)

	// Test iteration
	iter := db.NewIterator()
	defer iter.Release()

	count := 0
	seen := make(map[string]string)
	for iter.Next() {
		key := string(iter.Key())
		value := string(iter.Value())
		seen[key] = value
		count++
	}

	require.NoError(iter.Error())
	require.Equal(len(testData), count, "should iterate all keys")
	
	// Verify all keys seen
	for key, expectedValue := range testData {
		actualValue, exists := seen[key]
		require.True(exists, "key %s should be in iteration", key)
		require.Equal(expectedValue, actualValue)
	}
}

// TestFirewoodIteratorWithPending tests iterator sees pending writes
func TestFirewoodIteratorWithPending(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "firewood-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	log := logging.NoLog{}
	db, err := New(dbPath, nil, log)
	require.NoError(err)
	defer db.Close()

	// Add committed data
	err = db.Put([]byte("committed"), []byte("value1"))
	require.NoError(err)

	// Flush
	fwDB := db.(*Database)
	fwDB.pendingMu.Lock()
	err = fwDB.flushLocked()
	fwDB.pendingMu.Unlock()
	require.NoError(err)

	// Add pending data (no flush)
	err = db.Put([]byte("pending"), []byte("value2"))
	require.NoError(err)

	// Iterator should see both
	iter := db.NewIterator()
	defer iter.Release()

	seen := make(map[string]string)
	for iter.Next() {
		seen[string(iter.Key())] = string(iter.Value())
	}

	require.NoError(iter.Error())
	require.Equal(2, len(seen))
	require.Equal("value1", seen["committed"])
	require.Equal("value2", seen["pending"])
}

// TestFirewoodIteratorPendingOverride tests pending writes override committed
func TestFirewoodIteratorPendingOverride(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "firewood-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	log := logging.NoLog{}
	db, err := New(dbPath, nil, log)
	require.NoError(err)
	defer db.Close()

	// Add and flush committed value
	key := []byte("key")
	err = db.Put(key, []byte("old-value"))
	require.NoError(err)

	fwDB := db.(*Database)
	fwDB.pendingMu.Lock()
	err = fwDB.flushLocked()
	fwDB.pendingMu.Unlock()
	require.NoError(err)

	// Update with pending value
	err = db.Put(key, []byte("new-value"))
	require.NoError(err)

	// Iterator should see new value
	iter := db.NewIterator()
	defer iter.Release()

	found := false
	for iter.Next() {
		if string(iter.Key()) == "key" {
			require.Equal("new-value", string(iter.Value()))
			found = true
		}
	}

	require.NoError(iter.Error())
	require.True(found, "should find updated key")
}

// TestFirewoodIteratorWithPrefix tests prefix iteration
func TestFirewoodIteratorWithPrefix(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "firewood-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	log := logging.NoLog{}
	db, err := New(dbPath, nil, log)
	require.NoError(err)
	defer db.Close()

	// Add data with different prefixes
	testData := []struct {
		key   string
		value string
	}{
		{"user:alice", "data1"},
		{"user:bob", "data2"},
		{"config:timeout", "30"},
		{"config:retries", "3"},
	}

	for _, item := range testData {
		err = db.Put([]byte(item.key), []byte(item.value))
		require.NoError(err)
	}

	// Flush
	fwDB := db.(*Database)
	fwDB.pendingMu.Lock()
	err = fwDB.flushLocked()
	fwDB.pendingMu.Unlock()
	require.NoError(err)

	// Iterate with "user:" prefix
	iter := db.NewIteratorWithPrefix([]byte("user:"))
	defer iter.Release()

	count := 0
	for iter.Next() {
		key := string(iter.Key())
		require.Contains(key, "user:", "should only see user: keys")
		count++
	}

	require.NoError(iter.Error())
	require.Equal(2, count, "should find 2 user: keys")
}

// TestFirewoodIteratorDelete tests deleted keys are skipped
func TestFirewoodIteratorDelete(t *testing.T) {
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "firewood-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	log := logging.NoLog{}
	db, err := New(dbPath, nil, log)
	require.NoError(err)
	defer db.Close()

	// Add and flush data
	keys := [][]byte{
		[]byte("key1"),
		[]byte("key2"),
		[]byte("key3"),
	}

	for _, key := range keys {
		err = db.Put(key, []byte("value"))
		require.NoError(err)
	}

	fwDB := db.(*Database)
	fwDB.pendingMu.Lock()
	err = fwDB.flushLocked()
	fwDB.pendingMu.Unlock()
	require.NoError(err)

	// Delete key2 (pending)
	err = db.Delete([]byte("key2"))
	require.NoError(err)

	// Iterator should skip deleted key
	iter := db.NewIterator()
	defer iter.Release()

	seen := []string{}
	for iter.Next() {
		seen = append(seen, string(iter.Key()))
	}

	require.NoError(iter.Error())
	require.Equal(2, len(seen), "should see 2 keys (key2 deleted)")
	require.Contains(seen, "key1")
	require.Contains(seen, "key3")
	require.NotContains(seen, "key2")
}

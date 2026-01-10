// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"testing"

	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/stretchr/testify/require"
)

// TestSyncModeEdgeCases tests edge cases in sync mode handling
func TestSyncModeEdgeCases(t *testing.T) {
	require := require.New(t)

	t.Run("write invalid sync mode", func(t *testing.T) {
		db := memorydb.New()

		// Try to write invalid mode
		err := WriteSyncMode(db, "invalid")
		require.Error(err)
		require.Contains(err.Error(), "invalid sync mode")

		// Verify it wasn't written
		mode, err := ReadSyncMode(db)
		require.NoError(err)
		require.Equal("", mode)
	})

	t.Run("write empty sync mode", func(t *testing.T) {
		db := memorydb.New()

		// Empty string should be allowed (for clearing)
		err := WriteSyncMode(db, "")
		require.NoError(err)

		mode, err := ReadSyncMode(db)
		require.NoError(err)
		require.Equal("", mode)
	})

	t.Run("write valid sync modes", func(t *testing.T) {
		db := memorydb.New()

		// Test all valid modes
		validModes := []string{"state", "block", "hybrid"}
		for _, mode := range validModes {
			err := WriteSyncMode(db, mode)
			require.NoError(err, "mode: %s", mode)

			readMode, err := ReadSyncMode(db)
			require.NoError(err)
			require.Equal(mode, readMode)
		}
	})

	t.Run("read sync mode when not set", func(t *testing.T) {
		db := memorydb.New()

		mode, err := ReadSyncMode(db)
		require.NoError(err)
		require.Equal("", mode)
	})

	t.Run("delete sync mode", func(t *testing.T) {
		db := memorydb.New()

		// Write a mode
		err := WriteSyncMode(db, "state")
		require.NoError(err)

		// Delete it
		err = DeleteSyncMode(db)
		require.NoError(err)

		// Verify it's gone
		mode, err := ReadSyncMode(db)
		require.NoError(err)
		require.Equal("", mode)
	})

	t.Run("overwrite existing mode", func(t *testing.T) {
		db := memorydb.New()

		// Write state mode
		err := WriteSyncMode(db, "state")
		require.NoError(err)

		// Overwrite with block mode
		err = WriteSyncMode(db, "block")
		require.NoError(err)

		// Verify it changed
		mode, err := ReadSyncMode(db)
		require.NoError(err)
		require.Equal("block", mode)
	})
}

// TestHeightAccessorsEdgeCases tests edge cases in height accessors
func TestHeightAccessorsEdgeCases(t *testing.T) {
	require := require.New(t)

	t.Run("read height when not set", func(t *testing.T) {
		db := memorydb.New()

		height, err := ReadStateSyncLastHeight(db)
		require.NoError(err)
		require.Equal(uint64(0), height)

		progress, err := ReadBlockSyncProgress(db)
		require.NoError(err)
		require.Equal(uint64(0), progress)
	})

	t.Run("write and read max uint64", func(t *testing.T) {
		db := memorydb.New()

		maxHeight := uint64(18446744073709551615) // math.MaxUint64

		err := WriteStateSyncLastHeight(db, maxHeight)
		require.NoError(err)

		height, err := ReadStateSyncLastHeight(db)
		require.NoError(err)
		require.Equal(maxHeight, height)
	})

	t.Run("write zero height", func(t *testing.T) {
		db := memorydb.New()

		err := WriteBlockSyncProgress(db, 0)
		require.NoError(err)

		progress, err := ReadBlockSyncProgress(db)
		require.NoError(err)
		require.Equal(uint64(0), progress)
	})

	t.Run("overwrite height", func(t *testing.T) {
		db := memorydb.New()

		// Write initial height
		err := WriteStateSyncLastHeight(db, 1000)
		require.NoError(err)

		// Overwrite with new height
		err = WriteStateSyncLastHeight(db, 2000)
		require.NoError(err)

		// Verify it changed
		height, err := ReadStateSyncLastHeight(db)
		require.NoError(err)
		require.Equal(uint64(2000), height)
	})
}

// TestMissingCodeEdgeCases tests edge cases in missing code tracking
func TestMissingCodeEdgeCases(t *testing.T) {
	require := require.New(t)

	t.Run("add and check missing code", func(t *testing.T) {
		db := memorydb.New()

		// Create a test hash
		var codeHash [32]byte
		copy(codeHash[:], []byte("test_code_hash"))

		// Add it as missing
		err := AddMissingCode(db, codeHash, 12345)
		require.NoError(err)

		// Check if it exists
		has, err := HasMissingCode(db, codeHash)
		require.NoError(err)
		require.True(has)
	})

	t.Run("check non-existent missing code", func(t *testing.T) {
		db := memorydb.New()

		var codeHash [32]byte
		copy(codeHash[:], []byte("nonexistent"))

		has, err := HasMissingCode(db, codeHash)
		require.NoError(err)
		require.False(has)
	})

	t.Run("get missing code hashes", func(t *testing.T) {
		db := memorydb.New()

		// Add multiple missing codes (using common.Hash)
		hash1 := [32]byte{}
		hash2 := [32]byte{}
		copy(hash1[:], []byte("hash1"))
		copy(hash2[:], []byte("hash2"))

		err := AddMissingCode(db, hash1, 100)
		require.NoError(err)

		err = AddMissingCode(db, hash2, 200)
		require.NoError(err)

		// Get all missing code hashes
		hashes, err := GetMissingCodeHashes(db)
		require.NoError(err)
		require.Len(hashes, 2)

		// Check values directly
		val1, ok1 := hashes[hash1]
		require.True(ok1, "hash1 should be in map")
		require.Equal(uint64(100), val1)

		val2, ok2 := hashes[hash2]
		require.True(ok2, "hash2 should be in map")
		require.Equal(uint64(200), val2)
	})

	t.Run("multiple entries for same hash keeps lowest block", func(t *testing.T) {
		db := memorydb.New()

		var codeHash [32]byte
		copy(codeHash[:], []byte("duplicate"))

		// Add same hash at different blocks
		err := AddMissingCode(db, codeHash, 500)
		require.NoError(err)

		err = AddMissingCode(db, codeHash, 300)
		require.NoError(err)

		err = AddMissingCode(db, codeHash, 700)
		require.NoError(err)

		// Should keep the lowest block number
		hashes, err := GetMissingCodeHashes(db)
		require.NoError(err)
		require.Len(hashes, 1)
		require.Equal(uint64(300), hashes[codeHash])
	})

	t.Run("delete missing code", func(t *testing.T) {
		db := memorydb.New()

		var codeHash [32]byte
		copy(codeHash[:], []byte("to_delete"))

		// Add it
		err := AddMissingCode(db, codeHash, 1000)
		require.NoError(err)

		// Verify it exists
		has, err := HasMissingCode(db, codeHash)
		require.NoError(err)
		require.True(has)

		// Delete it
		err = DeleteMissingCode(db, codeHash, 1000)
		require.NoError(err)

		// Verify it's gone
		has, err = HasMissingCode(db, codeHash)
		require.NoError(err)
		require.False(has)
	})

	t.Run("clear all missing code", func(t *testing.T) {
		db := memorydb.New()

		// Add multiple missing codes
		for i := 0; i < 10; i++ {
			var hash [32]byte
			hash[0] = byte(i)
			err := AddMissingCode(db, hash, uint64(i*100))
			require.NoError(err)
		}

		// Verify they exist
		count, err := GetMissingCodeCount(db)
		require.NoError(err)
		require.Equal(10, count)

		// Clear all
		err = ClearMissingCode(db)
		require.NoError(err)

		// Verify all gone
		count, err = GetMissingCodeCount(db)
		require.NoError(err)
		require.Equal(0, count)
	})
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package migrate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestPerChainMigrator_BasicMigration(t *testing.T) {
	require := require.New(t)

	// Create source database with data for multiple chains
	sourceDB := memdb.New()

	// Create test chain IDs
	chainID1 := ids.GenerateTestID()
	chainID2 := ids.GenerateTestID()

	// Add keys with chain ID prefixes
	key1 := append(chainID1[:], []byte("key1")...)
	key2 := append(chainID1[:], []byte("key2")...)
	key3 := append(chainID2[:], []byte("key3")...)

	require.NoError(sourceDB.Put(key1, []byte("value1")))
	require.NoError(sourceDB.Put(key2, []byte("value2")))
	require.NoError(sourceDB.Put(key3, []byte("value3")))

	// Create target databases
	targetDB1 := memdb.New()
	targetDB2 := memdb.New()

	// Create migrator
	migrator := NewPerChainMigrator(sourceDB, logging.NoLog{})
	require.NoError(migrator.RegisterChainDB(chainID1, targetDB1))
	require.NoError(migrator.RegisterChainDB(chainID2, targetDB2))

	// Perform migration
	require.NoError(migrator.Migrate())

	// Verify keys were copied without chain ID prefix
	val1, err := targetDB1.Get([]byte("key1"))
	require.NoError(err)
	require.Equal([]byte("value1"), val1)

	val2, err := targetDB1.Get([]byte("key2"))
	require.NoError(err)
	require.Equal([]byte("value2"), val2)

	val3, err := targetDB2.Get([]byte("key3"))
	require.NoError(err)
	require.Equal([]byte("value3"), val3)

	// Verify statistics
	keysProcessed, keysCopied, keysSkipped := migrator.GetStatistics()
	require.Equal(3, keysProcessed)
	require.Equal(3, keysCopied)
	require.Equal(0, keysSkipped)
}

func TestPerChainMigrator_SkipsUnregisteredChains(t *testing.T) {
	require := require.New(t)

	sourceDB := memdb.New()

	chainID1 := ids.GenerateTestID()
	chainID2 := ids.GenerateTestID()

	// Add keys for two chains, but only register one
	key1 := append(chainID1[:], []byte("key1")...)
	key2 := append(chainID2[:], []byte("key2")...)

	require.NoError(sourceDB.Put(key1, []byte("value1")))
	require.NoError(sourceDB.Put(key2, []byte("value2")))

	targetDB1 := memdb.New()

	migrator := NewPerChainMigrator(sourceDB, logging.NoLog{})
	require.NoError(migrator.RegisterChainDB(chainID1, targetDB1))

	require.NoError(migrator.Migrate())

	// Verify only chain1 keys were copied
	val1, err := targetDB1.Get([]byte("key1"))
	require.NoError(err)
	require.Equal([]byte("value1"), val1)

	// Verify statistics
	keysProcessed, keysCopied, keysSkipped := migrator.GetStatistics()
	require.Equal(2, keysProcessed)
	require.Equal(1, keysCopied)
	require.Equal(1, keysSkipped)
}

func TestPerChainMigrator_SkipsShortKeys(t *testing.T) {
	require := require.New(t)

	sourceDB := memdb.New()
	chainID := ids.GenerateTestID()

	// Add a key that's too short to have a chain ID prefix
	shortKey := []byte("short")
	require.NoError(sourceDB.Put(shortKey, []byte("value")))

	// Add a valid key
	validKey := append(chainID[:], []byte("key")...)
	require.NoError(sourceDB.Put(validKey, []byte("validvalue")))

	targetDB := memdb.New()

	migrator := NewPerChainMigrator(sourceDB, logging.NoLog{})
	require.NoError(migrator.RegisterChainDB(chainID, targetDB))

	require.NoError(migrator.Migrate())

	// Verify valid key was copied
	val, err := targetDB.Get([]byte("key"))
	require.NoError(err)
	require.Equal([]byte("validvalue"), val)

	// Verify statistics
	keysProcessed, keysCopied, keysSkipped := migrator.GetStatistics()
	require.Equal(2, keysProcessed)
	require.Equal(1, keysCopied)
	require.Equal(1, keysSkipped)
}

func TestPerChainMigrator_Verification(t *testing.T) {
	require := require.New(t)

	sourceDB := memdb.New()
	chainID := ids.GenerateTestID()

	// Add test data
	key := append(chainID[:], []byte("key")...)
	require.NoError(sourceDB.Put(key, []byte("value")))

	targetDB := memdb.New()

	migrator := NewPerChainMigrator(sourceDB, logging.NoLog{})
	require.NoError(migrator.RegisterChainDB(chainID, targetDB))

	require.NoError(migrator.Migrate())

	// Verification should pass
	require.NoError(migrator.VerifyMigration())
}

func TestPerChainMigrator_VerificationDetectsMissingKey(t *testing.T) {
	require := require.New(t)

	sourceDB := memdb.New()
	chainID := ids.GenerateTestID()

	// Add test data
	key := append(chainID[:], []byte("key")...)
	require.NoError(sourceDB.Put(key, []byte("value")))

	targetDB := memdb.New()

	migrator := NewPerChainMigrator(sourceDB, logging.NoLog{})
	require.NoError(migrator.RegisterChainDB(chainID, targetDB))

	// Migrate
	require.NoError(migrator.Migrate())

	// Delete key from target to simulate incomplete migration
	require.NoError(targetDB.Delete([]byte("key")))

	// Verification should fail
	err := migrator.VerifyMigration()
	require.Error(err)
	require.Contains(err.Error(), "verification failed")
}

func TestPerChainMigrator_VerificationDetectsValueMismatch(t *testing.T) {
	require := require.New(t)

	sourceDB := memdb.New()
	chainID := ids.GenerateTestID()

	// Add test data
	key := append(chainID[:], []byte("key")...)
	require.NoError(sourceDB.Put(key, []byte("value")))

	targetDB := memdb.New()

	migrator := NewPerChainMigrator(sourceDB, logging.NoLog{})
	require.NoError(migrator.RegisterChainDB(chainID, targetDB))

	// Migrate
	require.NoError(migrator.Migrate())

	// Modify value in target to simulate corruption
	require.NoError(targetDB.Put([]byte("key"), []byte("wrong_value")))

	// Verification should fail
	err := migrator.VerifyMigration()
	require.Error(err)
	require.Contains(err.Error(), "verification failed")
}

func TestPerChainMigrator_RegisterChainDB_Errors(t *testing.T) {
	require := require.New(t)

	sourceDB := memdb.New()
	migrator := NewPerChainMigrator(sourceDB, logging.NoLog{})

	// Error on empty chain ID
	err := migrator.RegisterChainDB(ids.Empty, memdb.New())
	require.Error(err)
	require.Contains(err.Error(), "empty chain ID")

	// Error on nil database
	chainID := ids.GenerateTestID()
	err = migrator.RegisterChainDB(chainID, nil)
	require.Error(err)
	require.Contains(err.Error(), "cannot be nil")

	// Error on duplicate registration
	db := memdb.New()
	require.NoError(migrator.RegisterChainDB(chainID, db))
	err = migrator.RegisterChainDB(chainID, db)
	require.Error(err)
	require.Contains(err.Error(), "already registered")
}

func TestPerChainMigrator_Migrate_NoTargetDatabases(t *testing.T) {
	require := require.New(t)

	sourceDB := memdb.New()
	migrator := NewPerChainMigrator(sourceDB, logging.NoLog{})

	// Error when no target databases registered
	err := migrator.Migrate()
	require.Error(err)
	require.Contains(err.Error(), "no target databases")
}

func TestPerChainMigrator_LargeDataset(t *testing.T) {
	require := require.New(t)

	sourceDB := memdb.New()
	chainID := ids.GenerateTestID()

	// Add many keys
	numKeys := 1000
	for i := 0; i < numKeys; i++ {
		key := append(chainID[:], []byte{byte(i / 256), byte(i % 256)}...)
		value := []byte{byte(i), byte(i >> 8)}
		require.NoError(sourceDB.Put(key, value))
	}

	targetDB := memdb.New()

	migrator := NewPerChainMigrator(sourceDB, logging.NoLog{})
	require.NoError(migrator.RegisterChainDB(chainID, targetDB))

	require.NoError(migrator.Migrate())

	// Verify all keys were copied
	keysProcessed, keysCopied, _ := migrator.GetStatistics()
	require.Equal(numKeys, keysProcessed)
	require.Equal(numKeys, keysCopied)

	// Spot check some values
	for i := 0; i < numKeys; i += 100 {
		key := []byte{byte(i / 256), byte(i % 256)}
		val, err := targetDB.Get(key)
		require.NoError(err)
		require.Equal([]byte{byte(i), byte(i >> 8)}, val)
	}

	// Verify migration
	require.NoError(migrator.VerifyMigration())
}

func TestPerChainMigrator_EmptySourceDatabase(t *testing.T) {
	require := require.New(t)

	sourceDB := memdb.New()
	chainID := ids.GenerateTestID()
	targetDB := memdb.New()

	migrator := NewPerChainMigrator(sourceDB, logging.NoLog{})
	require.NoError(migrator.RegisterChainDB(chainID, targetDB))

	// Should succeed with empty database
	require.NoError(migrator.Migrate())

	keysProcessed, keysCopied, keysSkipped := migrator.GetStatistics()
	require.Equal(0, keysProcessed)
	require.Equal(0, keysCopied)
	require.Equal(0, keysSkipped)
}

func TestPerChainMigrator_MultipleChains(t *testing.T) {
	require := require.New(t)

	sourceDB := memdb.New()

	// Create 3 chains with different amounts of data
	chainID1 := ids.GenerateTestID()
	chainID2 := ids.GenerateTestID()
	chainID3 := ids.GenerateTestID()

	// Chain 1: 100 keys
	for i := 0; i < 100; i++ {
		key := append(chainID1[:], []byte{byte(i)}...)
		require.NoError(sourceDB.Put(key, []byte{byte(i)}))
	}

	// Chain 2: 200 keys
	for i := 0; i < 200; i++ {
		key := append(chainID2[:], []byte{byte(i / 256), byte(i % 256)}...)
		require.NoError(sourceDB.Put(key, []byte{byte(i)}))
	}

	// Chain 3: 50 keys
	for i := 0; i < 50; i++ {
		key := append(chainID3[:], []byte{byte(i)}...)
		require.NoError(sourceDB.Put(key, []byte{byte(i)}))
	}

	// Create target databases
	targetDB1 := memdb.New()
	targetDB2 := memdb.New()
	targetDB3 := memdb.New()

	migrator := NewPerChainMigrator(sourceDB, logging.NoLog{})
	require.NoError(migrator.RegisterChainDB(chainID1, targetDB1))
	require.NoError(migrator.RegisterChainDB(chainID2, targetDB2))
	require.NoError(migrator.RegisterChainDB(chainID3, targetDB3))

	require.NoError(migrator.Migrate())

	// Verify statistics
	keysProcessed, keysCopied, keysSkipped := migrator.GetStatistics()
	require.Equal(350, keysProcessed)
	require.Equal(350, keysCopied)
	require.Equal(0, keysSkipped)

	// Verify each database has correct number of keys
	count1, err := database.Count(targetDB1)
	require.NoError(err)
	require.Equal(100, count1)

	count2, err := database.Count(targetDB2)
	require.NoError(err)
	require.Equal(200, count2)

	count3, err := database.Count(targetDB3)
	require.NoError(err)
	require.Equal(50, count3)

	// Verify migration
	require.NoError(migrator.VerifyMigration())
}

func TestCopyBytes(t *testing.T) {
	require := require.New(t)

	// Test nil slice
	require.Nil(copyBytes(nil))

	// Test empty slice
	empty := []byte{}
	copiedEmpty := copyBytes(empty)
	require.NotNil(copiedEmpty)
	require.Len(copiedEmpty, 0)

	// Test normal slice
	original := []byte{1, 2, 3, 4, 5}
	copied := copyBytes(original)
	require.Equal(original, copied)

	// Verify they're different backing arrays
	copied[0] = 99
	require.NotEqual(original[0], copied[0])
	require.Equal(byte(1), original[0])
	require.Equal(byte(99), copied[0])
}

func TestBytesEqual(t *testing.T) {
	require := require.New(t)

	// Equal slices
	require.True(bytesEqual([]byte{1, 2, 3}, []byte{1, 2, 3}))

	// Different lengths
	require.False(bytesEqual([]byte{1, 2, 3}, []byte{1, 2}))

	// Different values
	require.False(bytesEqual([]byte{1, 2, 3}, []byte{1, 2, 4}))

	// Empty slices
	require.True(bytesEqual([]byte{}, []byte{}))
}

func TestMin(t *testing.T) {
	require := require.New(t)

	require.Equal(1, min(1, 2))
	require.Equal(1, min(2, 1))
	require.Equal(5, min(5, 5))
	require.Equal(-1, min(-1, 0))
}
